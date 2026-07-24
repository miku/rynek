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
// dance, and the re-spelling of input paths -- by making them framework
// concerns.
//
// A task is expressed as data:
//
//	var Tokens = func(c Config) rynek.Shell {
//		return rynek.Shell{
//			In:  rynek.Inputs{"in": Corpus(c)},
//			Out: c.path("tokens", "txt"),
//			Cmd: `tr 'A-Z ' 'a-z\n' < {in} | grep -v '^$' > {out}`,
//		}
//	}
//
// In maps a placeholder name to the upstream task that produces it. Each entry
// does double duty: it declares the dependency edge (so it appears in Requires)
// and binds {name} in Cmd to that task's output path -- the path is never
// written twice. {out} is bound to a temp file that is renamed into Out only on
// success, so a crash or cancellation never leaves a half-written artifact that
// later reads as complete.
//
// Identity is the output path (see Key). Because any parameter that changes an
// artifact's contents must also change its path, equal outputs are the same
// node -- which is exactly what the runner needs to dedup shared upstreams.
type Shell struct {
	Name string // optional label for keys/logs; identity still includes Out (see Key)
	In   Inputs // placeholder -> upstream task; {name} renders to its output path
	Out  string // output file path; {out} in Cmd renders to a temp, renamed on success
	Cmd  string // bash pipeline template with {out} and one {name} per In entry
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

// Output is the file this task produces, or nil when Out is empty (a pure
// side-effecting command with no tracked artifact).
func (s Shell) Output() Target {
	if s.Out == "" {
		return nil
	}
	return FileTarget{Path: s.Out}
}

// Key is the task's identity. It is always anchored on the output path, which
// already encodes every parameter that affects the artifact -- so equal outputs
// are the same node, which is what the runner needs to dedup shared upstreams.
// A Name, when set, is only a prefix for readability: it makes keys read like
// the reflection-derived ones ("Tokens(<path>)") without weakening identity, so
// two dates with the same Name still get distinct keys via their paths.
func (s Shell) Key() string {
	id := s.Out
	if id == "" {
		id = "Shell(" + s.Cmd + ")"
	}
	if s.Name != "" {
		return s.Name + "(" + id + ")"
	}
	return id
}

// Run renders the pipeline and executes it. Inputs bind to their upstream
// outputs; {out} binds to a temp file in Out's directory that is renamed into
// place on success (and removed on failure).
func (s Shell) Run(ctx context.Context) error {
	args := make(map[string]string, len(s.In)+1)
	for name, dep := range s.In {
		args[name] = Describe(dep.Output())
	}

	if s.Out == "" {
		return Cmd(ctx, s.Cmd, args)
	}

	dir := filepath.Dir(s.Out)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(s.Out)+".tmp-*")
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
	if err := os.Rename(tmp, s.Out); err != nil {
		return fmt.Errorf("promote %s: %w", s.Out, err)
	}
	return nil
}
