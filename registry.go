package rynek

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Params carries the values the CLI passes to a task constructor. Base is the
// structural artifact root (the "-" home directory); it shapes where output
// goes but never appears in a filename. Date is optional: one-off tasks can
// ignore it and key themselves statically. Extra holds the remaining string
// parameters (source, style, feed, ...), each of which does contribute to the
// conventional filename (see Path).
type Params struct {
	Base  string
	Date  time.Time
	Extra map[string]string
}

// Get returns Extra[key] or def if absent.
func (p Params) Get(key, def string) string {
	if v, ok := p.Extra[key]; ok {
		return v
	}
	return def
}

// HasDate reports whether a date was supplied (non-zero).
func (p Params) HasDate() bool { return !p.Date.IsZero() }

// Param documents a single task parameter: an entry the task reads from
// Params.Extra. It is surfaced by "rynek help <Task>" and can be supplied on the
// CLI with -p name=value.
type Param struct {
	Name    string
	Usage   string
	Default string
}

// Registration is the CLI-facing metadata for a registered task type: how to
// build it (New) plus documentation (Doc, Params) shown by "rynek help".
type Registration struct {
	Name   string
	Doc    string
	Params []Param
	New    func(Params) Task
}

// Option customizes a task Registration at Register time.
type Option func(*Registration)

// Doc sets a task's description, shown by "rynek help <Task>".
func Doc(s string) Option {
	return func(r *Registration) { r.Doc = s }
}

// WithParam documents a task parameter (an Extra key) with a usage string and
// default value. Repeat it once per parameter.
func WithParam(name, usage, def string) Option {
	return func(r *Registration) {
		r.Params = append(r.Params, Param{Name: name, Usage: usage, Default: def})
	}
}

var (
	regMu sync.RWMutex
	reg   = map[string]Registration{}
)

// Register wires a task name to a constructor so the CLI can build a root task
// from "rynek run <Name> ...". Optional Doc/WithParam options attach
// documentation. It panics on a duplicate name, which surfaces wiring mistakes
// at init time.
func Register(name string, ctor func(Params) Task, opts ...Option) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := reg[name]; dup {
		panic(fmt.Sprintf("rynek: task already registered: %s", name))
	}
	r := Registration{Name: name, New: ctor}
	for _, o := range opts {
		o(&r)
	}
	reg[name] = r
}

// Lookup builds a registered task by name.
func Lookup(name string, p Params) (Task, error) {
	regMu.RLock()
	r, ok := reg[name]
	regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown task: %s (known: %v)", name, Names())
	}
	return r.New(p), nil
}

// Info returns the registration metadata for a task name, for documentation.
func Info(name string) (Registration, bool) {
	regMu.RLock()
	r, ok := reg[name]
	regMu.RUnlock()
	return r, ok
}

// Names lists registered task names, sorted.
func Names() []string {
	regMu.RLock()
	names := make([]string, 0, len(reg))
	for n := range reg {
		names = append(names, n)
	}
	regMu.RUnlock()
	sort.Strings(names)
	return names
}
