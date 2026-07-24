// Command rynek is a thin CLI over the rynek task runner.
//
//	rynek run    <Task> [flags]   resolve the DAG and run what is missing
//	rynek deps   <Task> [flags]   print the resolved DAG (indented tree)
//	rynek status <Task> [flags]   report which targets exist / are missing
//	rynek list                    list registered task names
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

func main() {
	app := &cli.Command{
		Name:  "rynek",
		Usage: "a small file-driven DAG task runner",
		Commands: []*cli.Command{
			runCmd(),
			depsCmd(),
			statusCmd(),
			listCmd(),
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

// paramFlags are the parameter flags shared by run/deps/status. urfave/cli
// accepts them before or after the positional <Task>, so no manual interleaving
// is needed.
func paramFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "date", Usage: "task date (YYYY-MM-DD); optional"},
		&cli.StringFlag{Name: "home", Value: "rynek-work", Usage: "artifact home directory"},
	}
}

// rootTask reads the shared flags and the positional <Task> argument, then
// builds the root task from the registry.
func rootTask(cmd *cli.Command) (rynek.Task, error) {
	name := cmd.Args().First()
	if name == "" {
		return nil, fmt.Errorf("missing <Task> (try: rynek list)")
	}
	p := rynek.Params{Extra: map[string]string{"home": cmd.String("home")}}
	if d := cmd.String("date"); d != "" {
		parsed, err := time.Parse(dateLayout, d)
		if err != nil {
			return nil, fmt.Errorf("bad --date %q: %w", d, err)
		}
		p.Date = parsed
	}
	return rynek.Lookup(name, p)
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
	if seen[t.Key()] {
		fmt.Printf("%s%s (*)\n", indent, t.Key())
		return
	}
	seen[t.Key()] = true
	fmt.Printf("%s%s\n", indent, t.Key())
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
					fmt.Printf("%-12s %s\n", "wrapper", t.Key())
					continue
				}
				ok, err := out.Exists(ctx)
				state := "MISSING"
				if err != nil {
					state = "ERROR:" + err.Error()
				} else if ok {
					state = "exists"
				}
				fmt.Printf("%-12s %s\n", state, t.Key())
			}
			return nil
		},
	}
}

func listCmd() *cli.Command {
	return &cli.Command{
		Name:  "list",
		Usage: "list registered task names",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			names := rynek.Names()
			if len(names) == 0 {
				fmt.Println("no tasks registered")
				return nil
			}
			fmt.Println(strings.Join(names, "\n"))
			return nil
		},
	}
}
