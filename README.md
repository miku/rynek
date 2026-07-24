# rynek

A small, file-driven DAG task runner in Go, inspired by
[spotify/luigi](https://github.com/spotify/luigi). Extracted from the
[span](../span) / [siskin](../siskin) pipelines.

The whole model is three ideas:

1. A **task** has a stable identity (its `Key`, derived automatically from its
   parameters) and declared **dependencies** (`Requires()`) — together a DAG.
2. A task produces a **target**; *target exists == task complete* (idempotency).
3. A **runner** resolves the DAG and runs only what is missing, respecting
   dependency order and a concurrency limit.

The filesystem is the only state store. "Rebuild" means deleting an artifact and
running again. No daemon, no database. The library core is standard-library only;
the CLI adds [urfave/cli](https://github.com/urfave/cli) for argument parsing.

## Quick start

```sh
make build          # builds ./rynek and the library
./rynek list        # registered tasks (the bundled example pipeline)
./rynek deps Report # print the dependency tree
./rynek run Report  # run what's missing
./rynek status Report
```

The bundled `example` package is a cascading, diamond-shaped word-frequency
pipeline over `tr`/`sort`/`uniq` — enough to play with the runner immediately:

```
Corpus ── Tokens ─┬─ Unique ─────┐
                  └─ Frequencies ┴─ Report
```

`Tokens` and `Corpus` are shared upstreams, so they run exactly once. Try
`./rynek run Report --date 2026-07-24` for dated artifacts, or delete a file in
`.data/` and re-run to watch only the affected branch rebuild.

## Defining tasks

A task is any Go value implementing the interface:

```go
type Task interface {
	Requires() []Task     // upstream tasks; may be nil
	Output() Target       // completion marker; may be nil (wrapper task)
	Run(ctx context.Context) error
}
```

There is no `Key()` in the interface: identity is derived automatically. The
`rynek.Key(task)` function reflects over the concrete type name and its exported
fields, so a task's parameters become its key with zero boilerplate:

```go
type CrossrefSnapshot struct {
	Date time.Time
	Feed string
}
// rynek.Key(CrossrefSnapshot{Date: d, Feed: "2"}) == "CrossrefSnapshot(Date=2026-07-24,Feed=2)"
```

Embedded structs are flattened, so a shared parameter bundle (`type Config
struct{ Home string; Date time.Time }`, embedded in each task) contributes its
fields to every key — which is what makes equal task values dedup in the graph.
Fields must be **exported** to appear in the key. A task that wants a custom
identity string can opt in by implementing `Key() string` (the `Keyer`
interface), which overrides the reflection default — handy for a static
one-off key.

A task need not have a date: a one-off omits the field (or leaves it zero) and
writes to a static path. Register a constructor so the CLI can build it by name,
and attach documentation with `Doc`/`WithParam` — surfaced by `rynek help
<Task>` and `rynek list`:

```go
rynek.Register("Report",
	func(p rynek.Params) rynek.Task {
		return Report{Home: p.Get("home", ".data"), Date: p.Date}
	},
	rynek.Doc("combine the distinct-token count and the frequency histogram"),
	rynek.WithParam("repeat", "how many times to repeat the sample corpus", "1"),
)
```

Built-in flags (`--date`, `--home`) flow into `Params`; task-specific parameters
come in generically via repeatable `-p name=value` and are read with
`p.Get("name", default)`. A declared parameter's default is seeded automatically,
so `-p` only needs to be passed to override it. Because such a parameter usually
changes an artifact's contents, fold it into both the task key (put it in a field
so reflection picks it up) **and** the output path, or the exists-based
completeness check will treat a differently-parameterized file as done.

```
$ rynek help Report
NAME:
   Report - combine the distinct-token count and the frequency histogram
USAGE:
   rynek run Report [--date YYYY-MM-DD] [--home DIR] [-p repeat=...]
PARAMETERS:
   -p repeat=      how many times to repeat the sample corpus (default "1")
...
OUTPUT (with defaults):
   .data/report-static.txt
REQUIRES:
   Unique(Home=.data,Date=,Repeat=1)
   Frequencies(Home=.data,Date=,Repeat=1)
```

Helpers included: `FileTarget` (existence, with opt-in mtime staleness),
`Atomic` (temp-file + rename so partial output never reads as complete), and
`Cmd` (a `bash -o pipefail -c` wrapper with `{placeholder}` rendering for tasks
that shell out to external tools).

## Runner semantics

- **Completeness = target exists** by default. `--force` ignores existing
  outputs; `--dry-run` resolves and prints without running.
- **Diamonds run shared upstreams once** — each task key gets one memoized
  execution (`sync.Once`), so a shared dependency pulled by several parents runs
  a single time.
- **Bounded parallelism** — independent branches run concurrently up to
  `--workers` (default `NumCPU`); only `Run` calls are gated, not existence
  checks.
- **Fail-fast** — the first error cancels in-flight siblings via context; SIGINT
  cancels the run.
- **Cycle detection** — `Graph` validates the DAG up front and rejects cycles.

## Commands

```
rynek run    <Task> [--date YYYY-MM-DD] [--home DIR] [-p k=v ...] [--workers N] [--force] [--dry-run] [-v]
rynek deps   <Task> [--date ...] [--home DIR] [-p k=v ...]
rynek status <Task> [--date ...] [--home DIR]   # state + output path per task
rynek output <Task> [--date ...] [--home DIR] [--all]   # show the output path(s)
rynek list                                      # tasks + descriptions
rynek help   <Task>                             # params, output path, deps
```

## Development

```sh
make test     # unit tests (fake tasks/targets — no external tools)
make race     # race detector, hammers the diamond graph
make vet
```

See [notes/2026-07-24-proposal.md](notes/2026-07-24-proposal.md) for the design
rationale.
