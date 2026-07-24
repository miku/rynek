package rynek

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTarget flips to "exists" after the owning task runs. A counter records how
// many times the task actually ran, so tests can assert single execution.
type fakeTarget struct{ done *atomic.Bool }

func (f fakeTarget) Exists(context.Context) (bool, error) { return f.done.Load(), nil }

// fakeTask is an in-memory task with configurable key, deps, and behaviour.
type fakeTask struct {
	key   string
	deps  []Task
	runs  *atomic.Int64
	done  *atomic.Bool
	fail  bool
	noOut bool // wrapper task: Output() == nil
}

func newFake(key string, deps ...Task) *fakeTask {
	return &fakeTask{key: key, deps: deps, runs: &atomic.Int64{}, done: &atomic.Bool{}}
}

func (t *fakeTask) Key() string      { return t.key }
func (t *fakeTask) Requires() []Task { return t.deps }
func (t *fakeTask) Output() Target {
	if t.noOut {
		return nil
	}
	return fakeTarget{done: t.done}
}
func (t *fakeTask) Run(context.Context) error {
	t.runs.Add(1)
	if t.fail {
		return errors.New("boom")
	}
	t.done.Store(true)
	return nil
}

func quietRunner() *Runner {
	return &Runner{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestLinearChain(t *testing.T) {
	a := newFake("A")
	b := newFake("B", a)
	c := newFake("C", b)

	if err := quietRunner().Run(context.Background(), c); err != nil {
		t.Fatalf("run: %v", err)
	}
	for _, task := range []*fakeTask{a, b, c} {
		if got := task.runs.Load(); got != 1 {
			t.Errorf("%s ran %d times, want 1", task.key, got)
		}
	}
}

func TestDiamondRunsSharedUpstreamOnce(t *testing.T) {
	// D
	// | \
	// B  C
	// \ /
	//  A     A is shared; must run exactly once.
	a := newFake("A")
	b := newFake("B", a)
	c := newFake("C", a)
	d := newFake("D", b, c)

	// Hammer with high concurrency to shake out races (run with -race).
	r := quietRunner()
	r.Workers = 8
	if err := r.Run(context.Background(), d); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := a.runs.Load(); got != 1 {
		t.Fatalf("shared upstream A ran %d times, want 1", got)
	}
}

func TestSkipOnExists(t *testing.T) {
	a := newFake("A")
	a.done.Store(true) // pretend the output already exists

	if err := quietRunner().Run(context.Background(), a); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := a.runs.Load(); got != 0 {
		t.Errorf("A ran %d times, want 0 (output existed)", got)
	}
}

func TestForceRunsEvenIfComplete(t *testing.T) {
	a := newFake("A")
	a.done.Store(true)

	r := quietRunner()
	r.Force = true
	if err := r.Run(context.Background(), a); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := a.runs.Load(); got != 1 {
		t.Errorf("A ran %d times under --force, want 1", got)
	}
}

func TestDryRunNeverRuns(t *testing.T) {
	a := newFake("A")
	b := newFake("B", a)

	r := quietRunner()
	r.DryRun = true
	if err := r.Run(context.Background(), b); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := a.runs.Load() + b.runs.Load(); got != 0 {
		t.Errorf("tasks ran %d times under --dry-run, want 0", got)
	}
}

func TestFailurePropagates(t *testing.T) {
	a := newFake("A")
	a.fail = true
	b := newFake("B", a)

	err := quietRunner().Run(context.Background(), b)
	if err == nil {
		t.Fatal("expected error from failing dependency")
	}
	if b.runs.Load() != 0 {
		t.Errorf("B ran despite failed dependency A")
	}
}

func TestWrapperTaskWithNilOutput(t *testing.T) {
	a := newFake("A")
	wrap := newFake("Wrap", a)
	wrap.noOut = true

	if err := quietRunner().Run(context.Background(), wrap); err != nil {
		t.Fatalf("run: %v", err)
	}
	if a.runs.Load() != 1 {
		t.Errorf("dependency A of wrapper ran %d times, want 1", a.runs.Load())
	}
	if wrap.runs.Load() != 0 {
		t.Errorf("wrapper Run should not be called (nil Output)")
	}
}

func TestGraphDetectsCycle(t *testing.T) {
	a := newFake("A")
	b := newFake("B", a)
	a.deps = []Task{b} // A -> B -> A

	if _, err := Graph(a); err == nil {
		t.Fatal("expected cycle detection error")
	}
	if err := quietRunner().Run(context.Background(), a); err == nil {
		t.Fatal("Run should reject a cyclic graph")
	}
}

func TestGraphTopologicalOrder(t *testing.T) {
	a := newFake("A")
	b := newFake("B", a)
	c := newFake("C", b)

	order, err := Graph(c)
	if err != nil {
		t.Fatalf("graph: %v", err)
	}
	// Leaves first: A before B before C.
	pos := map[string]int{}
	for i, task := range order {
		pos[Key(task)] = i
	}
	if !(pos["A"] < pos["B"] && pos["B"] < pos["C"]) {
		t.Errorf("bad topological order: %v", keys(order))
	}
}

func keys(ts []Task) []string {
	var out []string
	for _, t := range ts {
		out = append(out, Key(t))
	}
	return out
}

// autoTask has no Key method, so it exercises reflection-derived keys. Only its
// exported fields (Tag, embedded Base) contribute to the key; the unexported
// bookkeeping fields are ignored, which is exactly what lets two independently
// constructed but value-equal upstreams dedup.
type Base struct{ Home string }

type autoTask struct {
	Base
	Tag  string
	deps []Task
	runs *atomic.Int64
	done *atomic.Bool
}

func (t autoTask) Requires() []Task          { return t.deps }
func (t autoTask) Output() Target            { return fakeTarget{done: t.done} }
func (t autoTask) Run(context.Context) error { t.runs.Add(1); t.done.Store(true); return nil }

func TestAutoKeyDerivation(t *testing.T) {
	// Embedded Base is flattened (Home), unexported fields are skipped.
	got := Key(autoTask{Base: Base{Home: "/data"}, Tag: "crossref"})
	if want := "autoTask(Home=/data,Tag=crossref)"; got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}

	// A task that implements Keyer wins over reflection.
	if got := Key(newFake("Explicit")); got != "Explicit" {
		t.Errorf("Keyer not honored: got %q", got)
	}
}

// snapshot mirrors a real dated task (like CrossrefSnapshot) for key tests.
type snapshot struct {
	Date time.Time
	Feed string
}

func (s snapshot) Requires() []Task          { return nil }
func (s snapshot) Output() Target            { return nil }
func (s snapshot) Run(context.Context) error { return nil }

func TestDateFormattingInKey(t *testing.T) {
	d := snapshot{Date: time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC), Feed: "2"}
	if got, want := Key(d), "snapshot(Date=2026-07-24,Feed=2)"; got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}
	// A zero date renders empty (the "undated / one-off" case).
	if got, want := Key(snapshot{Feed: "2"}), "snapshot(Date=,Feed=2)"; got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}
}

func TestAutoKeyDedupsDiamond(t *testing.T) {
	// Same as the diamond test, but with reflection-derived keys: two parents
	// each construct their own value-equal "A" upstream. It must still run once.
	runsA, doneA := &atomic.Int64{}, &atomic.Bool{}
	mkA := func() autoTask {
		return autoTask{Tag: "A", runs: runsA, done: doneA}
	}
	mk := func(tag string, deps ...Task) autoTask {
		return autoTask{Tag: tag, deps: deps, runs: &atomic.Int64{}, done: &atomic.Bool{}}
	}
	b := mk("B", mkA())
	c := mk("C", mkA())
	d := mk("D", b, c)

	r := quietRunner()
	r.Workers = 8
	if err := r.Run(context.Background(), d); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := runsA.Load(); got != 1 {
		t.Fatalf("auto-keyed shared upstream A ran %d times, want 1", got)
	}
}

func TestAtomicWriteAndFileTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "out.txt")

	ft := FileTarget{Path: path}
	if ok, _ := ft.Exists(context.Background()); ok {
		t.Fatal("target should not exist yet")
	}

	if err := Atomic(path, func(w io.Writer) error {
		_, err := io.WriteString(w, "hello")
		return err
	}); err != nil {
		t.Fatalf("atomic: %v", err)
	}

	if ok, err := ft.Exists(context.Background()); err != nil || !ok {
		t.Fatalf("target should exist after Atomic, ok=%v err=%v", ok, err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "hello" {
		t.Fatalf("content = %q err=%v", b, err)
	}

	// No stray temp files left behind.
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("expected exactly one file, got %d", len(entries))
	}
}
