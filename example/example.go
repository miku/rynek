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
// None of these tasks implement Key: rynek derives it from the type name and
// the exported Config fields (e.g. "Report(Home=rynek-work,Date=2026-07-24)"),
// so the shared Config embed dedups the diamond automatically.
//
// Importing this package (for its side-effecting init) is enough to make the
// tasks available to the CLI registry.
package example

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/miku/rynek"
)

// repeatParam documents the one task-specific parameter this pipeline exposes.
// Because it lives in the shared Config it flows into every task's key, so
// changing it correctly rebuilds the whole graph.
var repeatParam = rynek.WithParam("repeat", "how many times to repeat the sample corpus", "1")

func init() {
	rynek.Register("Corpus", func(p rynek.Params) rynek.Task { return Corpus{cfg(p)} },
		rynek.Doc("write a fixed sample corpus (a leaf task, no upstream)"), repeatParam)
	rynek.Register("Tokens", func(p rynek.Params) rynek.Task { return Tokens{cfg(p)} },
		rynek.Doc("lowercase the corpus into one token per line"), repeatParam)
	rynek.Register("Unique", func(p rynek.Params) rynek.Task { return Unique{cfg(p)} },
		rynek.Doc("count the number of distinct tokens"), repeatParam)
	rynek.Register("Frequencies", func(p rynek.Params) rynek.Task { return Frequencies{cfg(p)} },
		rynek.Doc("build the token frequency histogram, most frequent first"), repeatParam)
	rynek.Register("Report", func(p rynek.Params) rynek.Task { return Report{cfg(p)} },
		rynek.Doc("combine the distinct-token count and the frequency histogram"), repeatParam)
}

// Config is the shared parameter set threaded through the pipeline. Its fields
// are exported so rynek's reflection-derived Key can see them; embedding it in
// each task flattens Home/Date/Repeat into every task's key. Date is optional;
// when zero, artifacts land under a "static" prefix.
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

// Corpus writes a fixed sample text. It has no upstream -- a "one-off" style
// leaf task.
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
type Tokens struct{ Config }

func (t Tokens) Requires() []rynek.Task { return []rynek.Task{Corpus{t.Config}} }
func (t Tokens) Output() rynek.Target   { return rynek.FileTarget{Path: t.path("tokens", "txt")} }
func (t Tokens) Run(ctx context.Context) error {
	tmp := t.path("tokens", "txt") + ".tmp"
	if err := rynek.Cmd(ctx,
		`tr 'A-Z ' 'a-z\n' < {in} | grep -v '^$' > {out}`,
		map[string]string{"in": t.path("corpus", "txt"), "out": tmp},
	); err != nil {
		return err
	}
	return os.Rename(tmp, t.path("tokens", "txt"))
}

// Unique counts the number of distinct tokens.
type Unique struct{ Config }

func (t Unique) Requires() []rynek.Task { return []rynek.Task{Tokens{t.Config}} }
func (t Unique) Output() rynek.Target   { return rynek.FileTarget{Path: t.path("unique", "txt")} }
func (t Unique) Run(ctx context.Context) error {
	tmp := t.path("unique", "txt") + ".tmp"
	if err := rynek.Cmd(ctx,
		`sort -u < {in} | wc -l | tr -d ' ' > {out}`,
		map[string]string{"in": t.path("tokens", "txt"), "out": tmp},
	); err != nil {
		return err
	}
	return os.Rename(tmp, t.path("unique", "txt"))
}

// Frequencies is the token histogram, most frequent first.
type Frequencies struct{ Config }

func (t Frequencies) Requires() []rynek.Task { return []rynek.Task{Tokens{t.Config}} }
func (t Frequencies) Output() rynek.Target {
	return rynek.FileTarget{Path: t.path("frequencies", "txt")}
}
func (t Frequencies) Run(ctx context.Context) error {
	tmp := t.path("frequencies", "txt") + ".tmp"
	if err := rynek.Cmd(ctx,
		`sort < {in} | uniq -c | sort -rn > {out}`,
		map[string]string{"in": t.path("tokens", "txt"), "out": tmp},
	); err != nil {
		return err
	}
	return os.Rename(tmp, t.path("frequencies", "txt"))
}

// Report combines Unique and Frequencies. Its two dependencies share the Tokens
// upstream, forming the diamond.
type Report struct{ Config }

func (t Report) Requires() []rynek.Task {
	return []rynek.Task{Unique{t.Config}, Frequencies{t.Config}}
}
func (t Report) Output() rynek.Target { return rynek.FileTarget{Path: t.path("report", "txt")} }
func (t Report) Run(ctx context.Context) error {
	tmp := t.path("report", "txt") + ".tmp"
	if err := rynek.Cmd(ctx,
		`{ echo "distinct tokens: $(cat {uniq})"; echo "--- frequencies ---"; cat {freq}; } > {out}`,
		map[string]string{
			"uniq": t.path("unique", "txt"),
			"freq": t.path("frequencies", "txt"),
			"out":  tmp,
		},
	); err != nil {
		return err
	}
	return os.Rename(tmp, t.path("report", "txt"))
}
