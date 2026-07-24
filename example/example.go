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
// Importing this package (for its side-effecting init) is enough to make the
// tasks available to the CLI registry.
package example

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/miku/rynek"
)

func init() {
	rynek.Register("Corpus", func(p rynek.Params) rynek.Task { return Corpus{cfg(p)} })
	rynek.Register("Tokens", func(p rynek.Params) rynek.Task { return Tokens{cfg(p)} })
	rynek.Register("Unique", func(p rynek.Params) rynek.Task { return Unique{cfg(p)} })
	rynek.Register("Frequencies", func(p rynek.Params) rynek.Task { return Frequencies{cfg(p)} })
	rynek.Register("Report", func(p rynek.Params) rynek.Task { return Report{cfg(p)} })
}

// config is the shared parameter set threaded through the pipeline. Date is
// optional; when zero, artifacts land under a "static" prefix.
type config struct {
	Home string
	Date time.Time
}

func cfg(p rynek.Params) config {
	return config{Home: p.Get("home", ".rynek/data"), Date: p.Date}
}

// path builds "<home>/<name>-<prefix>.<ext>" where prefix is the date, if any.
func (c config) path(name, ext string) string {
	prefix := "static"
	if !c.Date.IsZero() {
		prefix = c.Date.Format("2006-01-02")
	}
	return filepath.Join(c.Home, fmt.Sprintf("%s-%s.%s", name, prefix, ext))
}

// keySuffix disambiguates task keys by parameters.
func (c config) keySuffix() string {
	if c.Date.IsZero() {
		return "static"
	}
	return c.Date.Format("2006-01-02")
}

// Corpus writes a fixed sample text. It has no upstream and (deliberately) no
// date requirement of its own -- a "one-off" style leaf task.
type Corpus struct{ config }

func (t Corpus) Key() string            { return "Corpus(" + t.keySuffix() + ")" }
func (t Corpus) Requires() []rynek.Task { return nil }
func (t Corpus) Output() rynek.Target   { return rynek.FileTarget{Path: t.path("corpus", "txt")} }
func (t Corpus) Run(ctx context.Context) error {
	const sample = "the quick brown fox the lazy dog the fox jumps the dog sleeps the quick fox\n"
	return rynek.Atomic(t.path("corpus", "txt"), func(w io.Writer) error {
		_, err := io.WriteString(w, sample)
		return err
	})
}

// Tokens lowercases the corpus and emits one word per line. Shared upstream.
type Tokens struct{ config }

func (t Tokens) Key() string            { return "Tokens(" + t.keySuffix() + ")" }
func (t Tokens) Requires() []rynek.Task { return []rynek.Task{Corpus{t.config}} }
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
type Unique struct{ config }

func (t Unique) Key() string            { return "Unique(" + t.keySuffix() + ")" }
func (t Unique) Requires() []rynek.Task { return []rynek.Task{Tokens{t.config}} }
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
type Frequencies struct{ config }

func (t Frequencies) Key() string            { return "Frequencies(" + t.keySuffix() + ")" }
func (t Frequencies) Requires() []rynek.Task { return []rynek.Task{Tokens{t.config}} }
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
type Report struct{ config }

func (t Report) Key() string { return "Report(" + t.keySuffix() + ")" }
func (t Report) Requires() []rynek.Task {
	return []rynek.Task{Unique{t.config}, Frequencies{t.config}}
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
