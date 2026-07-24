package rynek

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Params carries the values the CLI passes to a task constructor. Date is
// optional: one-off tasks can ignore it and key themselves statically. Extra
// holds any remaining string parameters (source, style, feed, ...).
type Params struct {
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

var (
	regMu sync.RWMutex
	reg   = map[string]func(Params) Task{}
)

// Register wires a task name to a constructor so the CLI can build a root task
// from "rynek run <Name> ...". It panics on a duplicate name, which surfaces
// wiring mistakes at init time.
func Register(name string, ctor func(Params) Task) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := reg[name]; dup {
		panic(fmt.Sprintf("rynek: task already registered: %s", name))
	}
	reg[name] = ctor
}

// Lookup builds a registered task by name.
func Lookup(name string, p Params) (Task, error) {
	regMu.RLock()
	ctor, ok := reg[name]
	regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown task: %s (known: %v)", name, Names())
	}
	return ctor(p), nil
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
