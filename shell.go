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
	// Name/P/Ext, plus the codec's suffix; set Out to use an explicit path.
	P     Params // parameters that drive the conventional output path
	Ext   string // output extension for the conventional path (default "out")
	Codec Codec  // transparent compression for output and inferred for inputs
	Out   string // explicit output path; overrides the convention when set
}

// out is the resolved output path: the explicit Out if given, otherwise the
// conventional BASE/Name/Name-<params>.Ext with the codec suffix appended.
// Empty only when neither an explicit Out nor a Name is set (a pure
// side-effecting command with no artifact).
func (s Shell) out() string {
	if s.Out != "" {
		return s.Out
	}
	if s.Name == "" {
		return ""
	}
	return s.P.Path(s.Name, s.Ext) + s.Codec.suffix()
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

// Run renders the pipeline and executes it. Each input binds to a shell
// expression yielding its upstream's (transparently decompressed) contents;
// {out} binds to a plaintext temp that is promoted into place on success --
// compressing through the codec first when one is set. A crash or cancellation
// leaves only temp files, never a half-written artifact that reads as complete.
func (s Shell) Run(ctx context.Context) error {
	args := make(map[string]string, len(s.In)+1)
	for name, dep := range s.In {
		args[name] = readExpr(Describe(dep.Output()))
	}

	out := s.out()
	if out == "" {
		return Cmd(ctx, s.Cmd, args)
	}

	dir := filepath.Dir(out)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	base := filepath.Base(out)

	// The command always writes plaintext to a temp. Input decompression is
	// streamed via readExpr, but output compression materializes the plaintext
	// first: a process-substitution writer would let bash rename before the
	// background compressor flushed. (A managed-FIFO streaming writer that we can
	// wait on is a later optimization.)
	plain, err := os.CreateTemp(dir, "."+base+".plain-*")
	if err != nil {
		return err
	}
	plainName := plain.Name()
	plain.Close()
	defer os.Remove(plainName) // no-op once renamed; cleanup otherwise

	args["out"] = plainName
	if err := Cmd(ctx, s.Cmd, args); err != nil {
		return err
	}

	if s.Codec == NoCodec {
		if err := os.Rename(plainName, out); err != nil {
			return fmt.Errorf("promote %s: %w", out, err)
		}
		return nil
	}

	comp, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return err
	}
	compName := comp.Name()
	comp.Close()
	defer os.Remove(compName) // no-op once renamed; cleanup otherwise

	if err := Cmd(ctx, s.Codec.compressExpr(), map[string]string{
		"src": shellQuote(plainName),
		"dst": shellQuote(compName),
	}); err != nil {
		return err
	}
	if err := os.Rename(compName, out); err != nil {
		return fmt.Errorf("promote %s: %w", out, err)
	}
	return nil
}
