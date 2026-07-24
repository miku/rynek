// Package rynek is a small, file-driven DAG task runner inspired by
// spotify/luigi. A Task has a stable identity and declared dependencies; it
// produces a Target whose presence signals completion. A Runner resolves the
// graph and runs only what is missing, respecting dependency order and a
// concurrency limit.
//
// The model is deliberately tiny: the filesystem is the state store, tasks
// shell out to external tools, and "rebuild" just means deleting an artifact
// and running again.
package rynek

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"sync"
	"time"
)

// Target is a build product whose presence signals completion.
type Target interface {
	// Exists reports whether the product is present (and, if the target
	// implements staleness, still fresh).
	Exists(ctx context.Context) (bool, error)
}

// Task is a unit of work with a stable identity and declared dependencies.
//
// A task need not have a date parameter: a "one-off" task can key itself with a
// static string and write to a static path. Parameters, when present, are
// struct fields folded into Key so that two task values with the same key are
// treated as the same node.
type Task interface {
	// Key uniquely identifies this task instance, parameters included.
	Key() string
	// Requires lists upstream tasks that must complete first. May be nil.
	Requires() []Task
	// Output is the target that marks this task complete. May be nil for pure
	// "wrapper" tasks that only group dependencies.
	Output() Target
	// Run produces Output. Called only when Output is absent (or Force).
	Run(ctx context.Context) error
}

// Runner resolves and executes a task DAG.
type Runner struct {
	Workers int          // max concurrent Run calls; default runtime.NumCPU()
	Force   bool         // ignore existing outputs, always Run
	DryRun  bool         // resolve + print, never Run
	Log     *slog.Logger // defaults to slog.Default()

	sem  chan struct{}
	mu   sync.Mutex
	memo map[string]*node
}

// node memoizes a single task execution keyed by Task.Key.
type node struct {
	once sync.Once
	err  error
}

// Run resolves and executes the DAG rooted at root. It first validates the
// graph (detecting cycles) via Graph, then executes recursively, running each
// distinct task key at most once.
func (r *Runner) Run(ctx context.Context, root Task) error {
	if r.Log == nil {
		r.Log = slog.Default()
	}
	if _, err := Graph(root); err != nil {
		return err
	}
	workers := r.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	r.sem = make(chan struct{}, max(1, workers))
	r.memo = make(map[string]*node)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return r.exec(ctx, root)
}

// exec memoizes so each task key runs at most once per invocation.
func (r *Runner) exec(ctx context.Context, t Task) error {
	r.mu.Lock()
	n, ok := r.memo[t.Key()]
	if !ok {
		n = &node{}
		r.memo[t.Key()] = n
	}
	r.mu.Unlock()

	n.once.Do(func() { n.err = r.execOnce(ctx, t) })
	return n.err
}

func (r *Runner) execOnce(ctx context.Context, t Task) error {
	// 1. Resolve dependencies concurrently. The first failure cancels its
	// siblings via the derived context.
	if deps := t.Requires(); len(deps) > 0 {
		dctx, cancel := context.WithCancel(ctx)
		defer cancel()

		var (
			wg    sync.WaitGroup
			once  sync.Once
			first error
		)
		for _, d := range deps {
			wg.Add(1)
			go func(d Task) {
				defer wg.Done()
				if err := r.exec(dctx, d); err != nil {
					once.Do(func() {
						first = err
						cancel()
					})
				}
			}(d)
		}
		wg.Wait()
		if first != nil {
			return first
		}
	}

	// 2. Completeness check (unless forced). A nil Output means a wrapper task
	// that is "complete" once its dependencies are.
	if !r.Force && t.Output() != nil {
		ok, err := t.Output().Exists(ctx)
		if err != nil {
			return fmt.Errorf("%s: checking output: %w", t.Key(), err)
		}
		if ok {
			r.Log.Debug("skip (complete)", "task", t.Key())
			return nil
		}
	}

	if t.Output() == nil {
		// Nothing to build; grouping node is done once deps are.
		return nil
	}

	if r.DryRun {
		r.Log.Info("would run", "task", t.Key())
		return nil
	}

	// 3. Acquire a worker slot and run.
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-r.sem }()

	start := time.Now()
	r.Log.Info("run", "task", t.Key())
	if err := t.Run(ctx); err != nil {
		r.Log.Error("failed", "task", t.Key(), "err", err)
		return fmt.Errorf("%s: %w", t.Key(), err)
	}
	r.Log.Info("done", "task", t.Key(), "took", time.Since(start).Round(time.Millisecond))
	return nil
}

// Graph returns the tasks reachable from root in dependency order (leaves
// first), deduplicated by Key. It returns an error if the graph contains a
// cycle. This is the topological view used by "deps" and "status", and by Run
// as an up-front validation step.
func Graph(root Task) ([]Task, error) {
	var order []Task
	done := map[string]bool{}   // fully visited
	onPath := map[string]bool{} // on the current DFS path (cycle guard)

	var visit func(t Task) error
	visit = func(t Task) error {
		k := t.Key()
		if onPath[k] {
			return fmt.Errorf("cycle detected involving %s", k)
		}
		if done[k] {
			return nil
		}
		onPath[k] = true
		for _, d := range t.Requires() {
			if err := visit(d); err != nil {
				return err
			}
		}
		onPath[k] = false
		done[k] = true
		order = append(order, t)
		return nil
	}
	if err := visit(root); err != nil {
		return nil, err
	}
	return order, nil
}
