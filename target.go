package rynek

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// FileTarget is a local file; existence == the file is present.
//
// The zero-ish value FileTarget{Path: "..."} uses pure existence semantics
// (luigi default). Set StaleIfOlderThan to also require the file be newer than
// the listed inputs (make semantics); this is opt-in because dated artifacts
// rarely need it and it invites surprising cascade rebuilds.
type FileTarget struct {
	Path             string
	StaleIfOlderThan []string
}

// Exists reports whether Path is present and, if StaleIfOlderThan is set, at
// least as new as every listed input.
func (f FileTarget) Exists(ctx context.Context) (bool, error) {
	fi, err := os.Stat(f.Path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(f.StaleIfOlderThan) == 0 {
		return true, nil
	}
	for _, in := range f.StaleIfOlderThan {
		ii, err := os.Stat(in)
		if err != nil {
			// Missing input can't make us stale; treat as fresh.
			continue
		}
		if ii.ModTime().After(fi.ModTime()) {
			return false, nil // an input is newer: stale, rebuild.
		}
	}
	return true, nil
}

// String makes FileTarget legible in logs and "status" output.
func (f FileTarget) String() string { return f.Path }

// Atomic writes a file via a temp file in the same directory plus rename, so a
// crash never leaves a half-written artifact that later reads as "complete".
// The parent directory is created if missing.
func Atomic(path string, fn func(w io.Writer) error) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	// On any error path, drop the temp file.
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()

	if err = fn(tmp); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
