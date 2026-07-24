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
// Four of the five tasks are rynek.Shell values: a placeholder->task map for
// inputs, an output path, and a bash pipeline. rynek supplies Requires, Output,
// the atomic temp-and-rename, and the identity (the output path), so the shared
// Config threaded through them dedups the diamond automatically. Corpus stays a
// hand-written Task to show the two styles coexist: it generates its content in
// Go rather than shelling out.
//
// Importing this package (for its side-effecting init) is enough to make the
// tasks available to the CLI registry.
package example

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/miku/rynek"
)

// repeatParam documents the one task-specific parameter this pipeline exposes.
// Because it lives in the shared Config it flows into every task's output path
// (and therefore its key), so changing it correctly rebuilds the whole graph.
var repeatParam = rynek.WithParam("repeat", "how many times to repeat the sample corpus", "1")

func init() {
	rynek.Register("Corpus", func(p rynek.Params) rynek.Task { return Corpus{cfg(p)} },
		rynek.Doc("write a fixed sample corpus (a leaf task, no upstream)"), repeatParam)
	rynek.Register("Tokens", func(p rynek.Params) rynek.Task { return Tokens(cfg(p)) },
		rynek.Doc("lowercase the corpus into one token per line"), repeatParam)
	rynek.Register("Unique", func(p rynek.Params) rynek.Task { return Unique(cfg(p)) },
		rynek.Doc("count the number of distinct tokens"), repeatParam)
	rynek.Register("Frequencies", func(p rynek.Params) rynek.Task { return Frequencies(cfg(p)) },
		rynek.Doc("build the token frequency histogram, most frequent first"), repeatParam)
	rynek.Register("Report", func(p rynek.Params) rynek.Task { return Report(cfg(p)) },
		rynek.Doc("combine the distinct-token count and the frequency histogram"), repeatParam)
}

// Config is the shared parameter set threaded through the pipeline. Embedding or
// passing it to each task builder flows Home/Date/Repeat into every output path.
// Date is optional; when zero, artifacts land under a "static" prefix.
type Config struct {
	Home   string
	Date   time.Time
	Repeat int
}

func cfg(p rynek.Params) Config {
	n, err := strconv.Atoi(p.Get("repeat", "1"))
	if err != nil || n < 1 {
		n = 1
	}
	return Config{Home: p.Get("home", ".data"), Date: p.Date, Repeat: n}
}

// prefix is the date component of an artifact path, or "static" when undated.
func (c Config) prefix() string {
	if c.Date.IsZero() {
		return "static"
	}
	return c.Date.Format("2006-01-02")
}

// path builds "<home>/<name>-<prefix>.<ext>". Any parameter that changes an
// artifact's contents must also change its path, otherwise the exists-based
// completeness check would treat a differently-parameterized file as done. The
// Repeat parameter therefore appears in the filename when non-default.
func (c Config) path(name, ext string) string {
	suffix := c.prefix()
	if c.Repeat != 1 {
		suffix += fmt.Sprintf("-r%d", c.Repeat)
	}
	return filepath.Join(c.Home, fmt.Sprintf("%s-%s.%s", name, suffix, ext))
}

// Corpus writes a fixed sample text. It generates content in Go rather than
// shelling out, so it stays a hand-written Task -- the escape hatch that
// rynek.Shell sits on top of. Atomic gives it the same crash-safe write the
// Shell tasks get for free.
type Corpus struct{ Config }

func (t Corpus) Requires() []rynek.Task { return nil }
func (t Corpus) Output() rynek.Target   { return rynek.FileTarget{Path: t.path("corpus", "txt")} }
func (t Corpus) Run(ctx context.Context) error {
	const line = "the quick brown fox the lazy dog the fox jumps the dog sleeps the quick fox\n"
	sample := strings.Repeat(line, t.Repeat)
	return rynek.Atomic(t.path("corpus", "txt"), func(w io.Writer) error {
		_, err := io.WriteString(w, sample)
		return err
	})
}

// Tokens lowercases the corpus and emits one word per line. Shared upstream.
func Tokens(c Config) rynek.Shell {
	return rynek.Shell{
		Name: "Tokens",
		In:   rynek.Inputs{"in": Corpus{c}},
		Out:  c.path("tokens", "txt"),
		Cmd:  `tr 'A-Z ' 'a-z\n' < {in} | grep -v '^$' > {out}`,
	}
}

// Unique counts the number of distinct tokens.
func Unique(c Config) rynek.Shell {
	return rynek.Shell{
		Name: "Unique",
		In:   rynek.Inputs{"in": Tokens(c)},
		Out:  c.path("unique", "txt"),
		Cmd:  `sort -u < {in} | wc -l | tr -d ' ' > {out}`,
	}
}

// Frequencies is the token histogram, most frequent first.
func Frequencies(c Config) rynek.Shell {
	return rynek.Shell{
		Name: "Frequencies",
		In:   rynek.Inputs{"in": Tokens(c)},
		Out:  c.path("frequencies", "txt"),
		Cmd:  `sort < {in} | uniq -c | sort -rn > {out}`,
	}
}

// Report combines Unique and Frequencies. Its two dependencies share the Tokens
// upstream, forming the diamond.
func Report(c Config) rynek.Shell {
	return rynek.Shell{
		Name: "Report",
		In: rynek.Inputs{
			"uniq": Unique(c),
			"freq": Frequencies(c),
		},
		Out: c.path("report", "txt"),
		Cmd: `{ echo "distinct tokens: $(cat {uniq})"; echo "--- frequencies ---"; cat {freq}; } > {out}`,
	}
}
