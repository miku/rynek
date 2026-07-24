// Package example defines a small, self-contained pipeline used in tests and as
// a playground for the rynek CLI. It shells out only to ubiquitous unix tools
// (tr, sort, uniq, wc) so it runs anywhere bash does.
//
// The graph is a cascade with a diamond:
//
//	Corpus ── Tokens ─┬─ Unique ─────┐
//	                  └─ Frequencies ┴─ Report
//
// Tokens (and Corpus) are shared upstreams pulled by two parents, so they must
// run exactly once. Report is the root you would run:
//
//	rynek run Report
//	rynek run Report --date 2026-07-24    # dated artifacts
//	rynek deps Report                     # print the graph
//	rynek status Report                   # which targets exist
//
// There is no per-task path code and no per-task Config: each builder takes the
// shared rynek.Params and lets rynek's convention place the output at
// <base>/<Task>/<Task>-<params>.<ext> (e.g. .data/Tokens/Tokens-repeat-1.txt).
// Because the path encodes the parameters, the shared params thread through the
// builders and the diamond dedups automatically. Four tasks are rynek.Shell
// values; Corpus stays a hand-written Task to show the two styles coexist -- it
// generates its content in Go rather than shelling out.
//
// Importing this package (for its side-effecting init) is enough to make the
// tasks available to the CLI registry.
package example

import (
	"context"
	"io"
	"strings"

	"github.com/miku/rynek"
)

// repeatParam documents the one task-specific parameter this pipeline exposes.
// Because it is an Extra parameter it flows into every task's output path (and
// therefore its key), so changing it correctly rebuilds the whole graph.
var repeatParam = rynek.WithParam("repeat", "how many times to repeat the sample corpus", "1")

func init() {
	rynek.Register("Corpus", func(c rynek.Ctx) rynek.Task { return Corpus{c} },
		rynek.Doc("write a fixed sample corpus (a leaf task, no upstream)"), repeatParam)
	rynek.Register("Tokens", func(c rynek.Ctx) rynek.Task { return Tokens(c) },
		rynek.Doc("lowercase the corpus into one token per line"), repeatParam)
	rynek.Register("Unique", func(c rynek.Ctx) rynek.Task { return Unique(c) },
		rynek.Doc("count the number of distinct tokens"), repeatParam)
	rynek.Register("Frequencies", func(c rynek.Ctx) rynek.Task { return Frequencies(c) },
		rynek.Doc("build the token frequency histogram, most frequent first"), repeatParam)
	rynek.Register("Report", func(c rynek.Ctx) rynek.Task { return Report(c) },
		rynek.Doc("combine the distinct-token count and the frequency histogram"), repeatParam)
}

// Corpus writes a fixed sample text. It generates content in Go rather than
// shelling out, so it stays a hand-written Task -- the escape hatch that
// rynek.Shell sits on top of. It reads its path and parameters from the ambient
// Ctx, and uses Atomic for the same crash-safe write the Shell tasks get.
type Corpus struct{ C rynek.Ctx }

func (t Corpus) path() string           { return t.C.Path("Corpus", t.C.Ext) }
func (t Corpus) Requires() []rynek.Task { return nil }
func (t Corpus) Output() rynek.Target   { return rynek.FileTarget{Path: t.path()} }
func (t Corpus) Key() string            { return "Corpus(" + t.path() + ")" }
func (t Corpus) Run(ctx context.Context) error {
	const line = "the quick brown fox the lazy dog the fox jumps the dog sleeps the quick fox\n"
	repeat := max(t.C.GetInt("repeat", 1), 1)
	sample := strings.Repeat(line, repeat)
	return rynek.Atomic(t.path(), func(w io.Writer) error {
		_, err := io.WriteString(w, sample)
		return err
	})
}

// Tokens lowercases the corpus and emits one word per line. Shared upstream.
func Tokens(c rynek.Ctx) rynek.Shell {
	return c.Shell(rynek.Shell{
		Name: "Tokens",
		In:   rynek.Inputs{"in": Corpus{c}},
		Cmd:  `tr 'A-Z ' 'a-z\n' < {in} | grep -v '^$' > {out}`,
	})
}

// Unique counts the number of distinct tokens.
func Unique(c rynek.Ctx) rynek.Shell {
	return c.Shell(rynek.Shell{
		Name: "Unique",
		In:   rynek.Inputs{"in": Tokens(c)},
		Cmd:  `sort -u < {in} | wc -l | tr -d ' ' > {out}`,
	})
}

// Frequencies is the token histogram, most frequent first.
func Frequencies(c rynek.Ctx) rynek.Shell {
	return c.Shell(rynek.Shell{
		Name: "Frequencies",
		In:   rynek.Inputs{"in": Tokens(c)},
		Cmd:  `sort < {in} | uniq -c | sort -rn > {out}`,
	})
}

// Report combines Unique and Frequencies. Its two dependencies share the Tokens
// upstream, forming the diamond.
func Report(c rynek.Ctx) rynek.Shell {
	return c.Shell(rynek.Shell{
		Name: "Report",
		In: rynek.Inputs{
			"uniq": Unique(c),
			"freq": Frequencies(c),
		},
		Cmd: `{ echo "distinct tokens: $(cat {uniq})"; echo "--- frequencies ---"; cat {freq}; } > {out}`,
	})
}
