package rynek

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Inputs maps a placeholder name to the upstream task that produces it. It is
// an alias for a plain map, so a literal, an existing map, or nil all work
// interchangeably wherever Shell.In is expected.
type Inputs = map[string]Task

// Shell is a declarative Task for the common case: pull a set of named inputs,
// run one shell pipeline, produce one file. It removes the boilerplate a
// hand-written Task repeats -- the Requires/Output/Run methods, the temp-file
// dance, the re-spelling of input paths, and the choice of where output lives --
// by making them framework concerns.
//
// A task is expressed as data:
//
//	func Tokens(p rynek.Params) rynek.Shell {
//		return rynek.Shell{
//			Name: "Tokens",
//			In:   rynek.Inputs{"in": Corpus{p}},
//			Cmd:  `tr 'A-Z ' 'a-z\n' < {in} | grep -v '^$' > {out}`,
//			P:    p,
//		}
//	}
//
// In maps a placeholder name to the upstream task that produces it. Each entry
// does double duty: it declares the dependency edge (so it appears in Requires)
// and binds {name} in Cmd to that task's output path -- the path is never
// written twice. {out} binds to a temp file that is renamed into the output only
// on success, so a crash or cancellation never leaves a half-written artifact
// that later reads as complete.
//
// The output path is by default derived by convention from Name, P, and Ext
// (see Params.Path): BASE/Name/Name-<params>.Ext. Set Out to override it. Either
// way the path is the task's identity (see Key): equal outputs are the same
// node, which is what the runner needs to dedup shared upstreams.
type Shell struct {
	Name string // task label; the default output stem and subdirectory
	In   Inputs // placeholder -> upstream task; {name} renders to its output path
	Cmd  string // bash pipeline template with {out} and one {name} per In entry

	// Output location. By default it is the conventional path derived from
	// Name/P/Ext; set Out to use an explicit path instead.
	P   Params // parameters that drive the conventional output path
	Ext string // output extension for the conventional path (default "out")
	Out string // explicit output path; overrides the convention when set
}

// out is the resolved output path: the explicit Out if given, otherwise the
// conventional BASE/Name/Name-<params>.Ext. Empty only when neither an explicit
// Out nor a Name is set (a pure side-effecting command with no artifact).
func (s Shell) out() string {
	if s.Out != "" {
		return s.Out
	}
	if s.Name == "" {
		return ""
	}
	ext := s.Ext
	if ext == "" {
		ext = "out"
	}
	return s.P.Path(s.Name, ext)
}

// Requires returns the upstream tasks, deduplicated and ordered by placeholder
// name so the resolved graph prints stably.
func (s Shell) Requires() []Task {
	if len(s.In) == 0 {
		return nil
	}
	names := make([]string, 0, len(s.In))
	for name := range s.In {
		names = append(names, name)
	}
	sort.Strings(names)
	var (
		out  []Task
		seen = map[string]bool{}
	)
	for _, name := range names {
		t := s.In[name]
		if k := Key(t); !seen[k] {
			seen[k] = true
			out = append(out, t)
		}
	}
	return out
}

// Output is the file this task produces, or nil when it has no output path (a
// pure side-effecting command).
func (s Shell) Output() Target {
	if o := s.out(); o != "" {
		return FileTarget{Path: o}
	}
	return nil
}

// Key is the task's identity. It is anchored on the output path, which already
// encodes every parameter that affects the artifact -- so equal outputs are the
// same node, which is what the runner needs to dedup shared upstreams. Name,
// when set, is only a prefix for readability: it makes keys read like the
// reflection-derived ones ("Tokens(<path>)") without weakening identity, since
// two runs with the same Name but different parameters still differ by path.
func (s Shell) Key() string {
	id := s.out()
	if id == "" {
		id = "Shell(" + s.Cmd + ")"
	}
	if s.Name != "" {
		return s.Name + "(" + id + ")"
	}
	return id
}

// Run renders the pipeline and executes it. Inputs bind to their upstream
// outputs; {out} binds to a temp file in the output's directory that is renamed
// into place on success (and removed on failure).
func (s Shell) Run(ctx context.Context) error {
	args := make(map[string]string, len(s.In)+1)
	for name, dep := range s.In {
		args[name] = Describe(dep.Output())
	}

	out := s.out()
	if out == "" {
		return Cmd(ctx, s.Cmd, args)
	}

	dir := filepath.Dir(out)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(out)+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	f.Close()
	defer os.Remove(tmp) // no-op after a successful rename; cleanup on failure

	args["out"] = tmp
	if err := Cmd(ctx, s.Cmd, args); err != nil {
		return err
	}
	if err := os.Rename(tmp, out); err != nil {
		return fmt.Errorf("promote %s: %w", out, err)
	}
	return nil
}
