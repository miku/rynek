package rynek

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// DefaultBase is the artifact root used when Params.Base is empty.
const DefaultBase = ".data"

// Path returns the conventional artifact path for a task, following a
// convention-over-configuration layout:
//
//	<base>/<task>/<task>[-<key>-<value>...].<ext>
//
// The parameters that vary an artifact -- the date, then each Extra entry in
// sorted order -- are folded into the filename, so equal parameters resolve to
// the same path and the exists-based completeness check works without any
// per-task path code. Base is structural: it names the root directory and never
// appears in the filename. Values are slugged so they are always safe path
// components.
//
// This is only the default. A task that needs a bespoke layout can ignore Path
// and set Shell.Out (or FileTarget.Path) to anything it likes.
func (p Params) Path(task, ext string) string {
	base := p.Base
	if base == "" {
		base = DefaultBase
	}
	stem := slug(task)
	if parts := p.pathParts(); len(parts) > 0 {
		stem += "-" + strings.Join(parts, "-")
	}
	return filepath.Join(base, slug(task), stem+"."+ext)
}

// pathParts returns the filename-varying parameters as an ordered, slugged
// [key, value, key, value, ...] list: the date first (when set), then Extra
// keys sorted for determinism. Empty values are skipped.
func (p Params) pathParts() []string {
	var parts []string
	if !p.Date.IsZero() {
		parts = append(parts, "date", p.Date.Format("2006-01-02"))
	}
	keys := make([]string, 0, len(p.Extra))
	for k := range p.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := p.Extra[k]
		if v == "" {
			continue
		}
		parts = append(parts, slug(k), slug(v))
	}
	return parts
}

// GetInt reads Extra[key] as an int, returning def when absent or unparseable.
func (p Params) GetInt(key string, def int) int {
	v, ok := p.Extra[key]
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// slug keeps a value to characters that are safe and legible in a path
// component (letters, digits, and -._), replacing anything else with '_' so a
// parameter value can never break out of or confuse the directory hierarchy.
func slug(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '.' || r == '_' || r == '-':
			return r
		default:
			return '_'
		}
	}, s)
}
