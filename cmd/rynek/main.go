// Command rynek is a thin CLI over the rynek task runner.
//
//	rynek run    <Task> [flags]   resolve the DAG and run what is missing
//	rynek deps   <Task> [flags]   print the resolved DAG (indented tree)
//	rynek status <Task> [flags]   report which targets exist / are missing
//	rynek output <Task> [flags]   show the output path(s) of a task
//	rynek list                    list registered tasks and descriptions
//	rynek help   <Task>           document a task: params, output, deps
//
// Tasks are contributed by imported packages via rynek.Register. This binary
// imports the bundled example pipeline so it does something useful out of the
// box; a real deployment would import its own task package instead.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/miku/rynek"
	_ "github.com/miku/rynek/example" // register example tasks
)

const dateLayout = "2006-01-02"

// projectExt and projectCodec are this binary's artifact defaults, applied to
// every task via the build Ctx unless a task sets its own. Enabling zstd here is
// the whole opt-in: no task code changes, intermediates become .txt.zst, and
// downstream inputs are decompressed transparently.
const projectExt = "txt"

var projectCodec = rynek.Zstd

func main() {
	app := &cli.Command{
		Name:  "rynek",
		Usage: "a small file-driven DAG task runner",
		// Replace the built-in help command with our own so that
		// "rynek help <Task>" documents a task, while still handling
		// "rynek help <command>" and "rynek help".
		HideHelpCommand: true,
		Commands: []*cli.Command{
			runCmd(),
			depsCmd(),
			statusCmd(),
			outputCmd(),
			listCmd(),
			helpCmd(),
		},
	}
	// SIGINT cancels the run context; in-flight external processes receive
	// SIGKILL via exec.CommandContext in the tasks.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := app.Run(ctx, os.Args); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// paramFlags are the parameter flags shared by run/deps/status/output.
// urfave/cli accepts them before or after the positional <Task>, so no manual
// interleaving is needed. Task-specific parameters are passed generically via
// repeatable -p name=value; run "rynek help <Task>" to see which a task reads.
func paramFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "date", Usage: "task date (YYYY-MM-DD); optional"},
		&cli.StringFlag{Name: "home", Value: ".data", Usage: "artifact home directory"},
		&cli.StringSliceFlag{Name: "param", Aliases: []string{"p"}, Usage: "task parameter, name=value (repeatable)"},
	}
}

// rootTask reads the shared flags and the positional <Task> argument, then
// builds the root task from the registry. It seeds each documented parameter's
// default into Extra, then applies -p overrides, so a task's declared defaults
// take effect even when the flag is omitted.
func rootTask(cmd *cli.Command) (rynek.Task, error) {
	name := cmd.Args().First()
	if name == "" {
		return nil, fmt.Errorf("missing <Task> (try: rynek list)")
	}
	extra := map[string]string{}
	if info, ok := rynek.Info(name); ok {
		for _, pr := range info.Params {
			if pr.Default != "" {
				extra[pr.Name] = pr.Default
			}
		}
	}
	for _, kv := range cmd.StringSlice("param") {
		k, v, found := strings.Cut(kv, "=")
		if !found {
			return nil, fmt.Errorf("bad -p %q, want name=value", kv)
		}
		extra[k] = v
	}
	p := rynek.Params{Base: cmd.String("home"), Extra: extra}
	if d := cmd.String("date"); d != "" {
		parsed, err := time.Parse(dateLayout, d)
		if err != nil {
			return nil, fmt.Errorf("bad --date %q: %w", d, err)
		}
		p.Date = parsed
	}
	return rynek.Lookup(name, rynek.Ctx{Params: p, Ext: projectExt, Codec: projectCodec})
}

func runCmd() *cli.Command {
	return &cli.Command{
		Name:      "run",
		Usage:     "resolve the DAG and run what is missing",
		ArgsUsage: "<Task>",
		Flags: append(paramFlags(),
			&cli.IntFlag{Name: "workers", Usage: "max concurrent Run calls (0 = NumCPU)"},
			&cli.BoolFlag{Name: "force", Usage: "ignore existing outputs, always run"},
			&cli.BoolFlag{Name: "dry-run", Usage: "resolve and print, never run"},
			&cli.BoolFlag{Name: "v", Usage: "verbose (debug) logging"},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			root, err := rootTask(cmd)
			if err != nil {
				return err
			}
			level := slog.LevelInfo
			if cmd.Bool("v") {
				level = slog.LevelDebug
			}
			logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

			r := &rynek.Runner{
				Workers: cmd.Int("workers"),
				Force:   cmd.Bool("force"),
				DryRun:  cmd.Bool("dry-run"),
				Log:     logger,
			}
			return r.Run(ctx, root)
		},
	}
}

func depsCmd() *cli.Command {
	return &cli.Command{
		Name:      "deps",
		Usage:     "print the resolved DAG (indented tree)",
		ArgsUsage: "<Task>",
		Flags:     paramFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			root, err := rootTask(cmd)
			if err != nil {
				return err
			}
			printTree(root, "", map[string]bool{})
			return nil
		},
	}
}

// printTree renders the dependency graph as an indented tree, marking a node
// already shown elsewhere so diamonds stay readable.
func printTree(t rynek.Task, indent string, seen map[string]bool) {
	if seen[rynek.Key(t)] {
		fmt.Printf("%s%s (*)\n", indent, rynek.Key(t))
		return
	}
	seen[rynek.Key(t)] = true
	fmt.Printf("%s%s\n", indent, rynek.Key(t))
	for _, d := range t.Requires() {
		printTree(d, indent+"  ", seen)
	}
}

func statusCmd() *cli.Command {
	return &cli.Command{
		Name:      "status",
		Usage:     "report which targets exist / are missing",
		ArgsUsage: "<Task>",
		Flags:     paramFlags(),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			root, err := rootTask(cmd)
			if err != nil {
				return err
			}
			order, err := rynek.Graph(root)
			if err != nil {
				return err
			}
			for _, t := range order {
				out := t.Output()
				if out == nil {
					fmt.Printf("%-8s %-44s\n", "wrapper", rynek.Key(t))
					continue
				}
				ok, err := out.Exists(ctx)
				state := "MISSING"
				if err != nil {
					state = "ERROR:" + err.Error()
				} else if ok {
					state = "exists"
				}
				fmt.Printf("%-8s %-44s %s\n", state, rynek.Key(t), rynek.Describe(out))
			}
			return nil
		},
	}
}

func outputCmd() *cli.Command {
	return &cli.Command{
		Name:      "output",
		Usage:     "show the output path of a task",
		ArgsUsage: "<Task>",
		Flags: append(paramFlags(),
			&cli.BoolFlag{Name: "all", Aliases: []string{"a"}, Usage: "show outputs for the whole DAG, not just the root"},
		),
		Action: func(ctx context.Context, cmd *cli.Command) error {
			root, err := rootTask(cmd)
			if err != nil {
				return err
			}
			if !cmd.Bool("all") {
				out := root.Output()
				if out == nil {
					return fmt.Errorf("%s has no output (wrapper task)", rynek.Key(root))
				}
				fmt.Println(rynek.Describe(out))
				return nil
			}
			order, err := rynek.Graph(root)
			if err != nil {
				return err
			}
			for _, t := range order {
				if out := t.Output(); out != nil {
					fmt.Println(rynek.Describe(out))
				}
			}
			return nil
		},
	}
}

func helpCmd() *cli.Command {
	return &cli.Command{
		Name:      "help",
		Aliases:   []string{"h"},
		Usage:     "show help for rynek, a command, or a task",
		ArgsUsage: "[command|Task]",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			root := cmd.Root()
			topic := cmd.Args().First()
			if topic == "" {
				return cli.ShowRootCommandHelp(root)
			}
			if _, ok := rynek.Info(topic); ok {
				return printTaskHelp(topic)
			}
			for _, c := range root.Commands {
				if c.HasName(topic) {
					return cli.ShowCommandHelp(ctx, root, topic)
				}
			}
			return fmt.Errorf("unknown help topic %q (try: rynek list, or rynek help)", topic)
		},
	}
}

// printTaskHelp renders a registered task's documentation: its description,
// declared parameters, the built-in flags, and -- by building the task with
// default parameters -- a concrete example of its output path and dependencies.
func printTaskHelp(name string) error {
	info, ok := rynek.Info(name)
	if !ok {
		return fmt.Errorf("unknown task: %s (try: rynek list)", name)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "NAME:\n   %s", info.Name)
	if info.Doc != "" {
		fmt.Fprintf(&b, " - %s", info.Doc)
	}
	b.WriteString("\n\nUSAGE:\n   rynek run " + info.Name + " [--date YYYY-MM-DD] [--home DIR]")
	for _, pr := range info.Params {
		fmt.Fprintf(&b, " [-p %s=...]", pr.Name)
	}
	b.WriteString("\n\nPARAMETERS:\n")
	if len(info.Params) == 0 {
		b.WriteString("   (none beyond the built-in flags below)\n")
	}
	for _, pr := range info.Params {
		def := ""
		if pr.Default != "" {
			def = fmt.Sprintf(" (default %q)", pr.Default)
		}
		fmt.Fprintf(&b, "   -p %-12s %s%s\n", pr.Name+"=", pr.Usage, def)
	}
	b.WriteString("\nBUILT-IN FLAGS:\n")
	b.WriteString("   --date YYYY-MM-DD   run date; optional\n")
	b.WriteString("   --home DIR          artifact home directory\n")

	// Build the task with defaults to show a concrete output path and deps.
	task := info.New(defaultCtx(info))
	if out := task.Output(); out != nil {
		fmt.Fprintf(&b, "\nOUTPUT (with defaults):\n   %s\n", rynek.Describe(out))
	} else {
		b.WriteString("\nOUTPUT:\n   (none -- wrapper task)\n")
	}
	if deps := task.Requires(); len(deps) > 0 {
		b.WriteString("\nREQUIRES:\n")
		for _, d := range deps {
			fmt.Fprintf(&b, "   %s\n", rynek.Key(d))
		}
	}
	fmt.Print(b.String())
	return nil
}

// defaultCtx builds the Ctx a task would receive with no flags: the default
// home, the project extension, and each documented parameter's default.
func defaultCtx(info rynek.Registration) rynek.Ctx {
	extra := map[string]string{}
	for _, pr := range info.Params {
		extra[pr.Name] = pr.Default
	}
	return rynek.Ctx{Params: rynek.Params{Base: rynek.DefaultBase, Extra: extra}, Ext: projectExt, Codec: projectCodec}
}

func listCmd() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "list registered tasks and their descriptions",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			names := rynek.Names()
			if len(names) == 0 {
				fmt.Println("no tasks registered")
				return nil
			}
			for _, n := range names {
				info, _ := rynek.Info(n)
				if info.Doc != "" {
					fmt.Printf("%-14s %s\n", n, info.Doc)
				} else {
					fmt.Println(n)
				}
			}
			fmt.Println("\nrun 'rynek help <Task>' for parameters and output path")
			return nil
		},
	}
}
