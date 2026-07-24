# A note on data processing and decomposition

A machine does not care about the task decomposition, only about instructions.
Everything above an instruction is an aid to the programmer to express
requirements.

A problem can be decomposed into many different things: Temporally, into a
sequence of steps, spacially into a set of components. A component can be
specific to a language (e.g. a Python model, a Go package) or a systems
abstraction, like an API boundary. The classic pipe and filter design uses a
program as a basic building block.

The various programming paradigms reflect this decompositions: objects,
compilation units, functions, assertions. Unix is not a programming language,
yet a shell glues tools, like a compile glues components together.

For code, the decompositions are better known, albeit not uncontested and still
source of many discussions. Code decomposes hierarchically, program, components
and packages, functions, variables.

We could enumerate all functions we know about. We could embed them into a
semantic space and we could plot them out.

We could draw dependency graphs of all functions we know. A function that calls
another function. The call graph relates code together. We can apply all graph
algorithms on the call graph.

Some questions to ask on the call graph, e.g. for a programming language: What
are the most used and least used functions within the language, may it be the
language proper or the ecosystem.

Data dependendencies exist since data exists, but the problem seemed to have a
bit younger history.

There is a raw input layer. Data, that is almost unchanged from the state it
was first acquired or produced. That raw layer expands rapidly.

Data will have a type, a set of values that we associate labels with. Because
the world produces a wide range of data, we are able to combine, mix and match
an ever increasing amount of types.

Everything, that is not raw is derived. Data can be derived from one or more
data sources. Data can be derived from raw or derived data sources.

Before data can be derived, all data dependencies have to be met.

If all inputs to a function or a derivation are known, the data artefact is
reproducible.

If the derivation contains any kind of random element relevant to the output,
the derviation is not reproducible, unless the random source uses a fixed seed.

Data derivations can be anything, interesting and very valuable.

Algorithms work on data. Data needs to be provided or derived.

We can express the derivations as a dependency graph. Most of the time, a data
dependency needs to be materialized.

All data derivations can be considered a cache. There is literally nothing but
raw data and cached computation.

Everything can be derived again from raw data, but the process may be too
involved to be feasible.

For most larger scale derivations, skipping intermediate manifestations are not
feasible.

Once data is written, it is never changed. Raw data never changes, or it can be
considered new data.

The world can store only so much raw data. The capacity expands but it has a
limit.  A tiny fraction of all possible derivations will be manifest.

Code dependencies are complex, yet, they are tiny compared to data dependencies
in terms of physical size. A complex project maybe a couple of megabytes, a
data-intensive task may be petabytes or exabytes.

Data can have any kind of useful unit, a record, a file (a set of record), a
collection (a set of files), a set of collections, and so on.

Data can have metadata, but we are more likely to call everything data, to keep
the conceptual structure flat.

Interesting programs usually come with interesting data sets. It will be much
easier to write an interesting program based on an interesting dataset that it
is to write it without.

Data is the empirical world. The empirical world does not lie, but it can miss
things.

A dataset can be decomposed into a smaller parts. A dataset derivation can be
composed into a set of smaller tasks. The derivation and the dataset are two
variants of the same. The one represents the result the other the process.

The result and the process are interchangable. If I know the result, I can
reverse engineer a process that yields the result. If I know the process, I can
run it and derive the result.

Going from the result to the process is more difficult. It can be much more difficult.

If I would know all inputs to a black box process and the result, the mapping
would relate to an unknown function, that relates the input to the output.

An LLM is a machine, that can explore a black box.

# misc

The preceding notes state a position. This section is meant to make it *useful*:
to sharpen the data-derivation problem into questions we can answer, and to give
us a way to judge how faithfully a real system (make, luigi, dvc, bazel, airflow,
dagster, prefect, nix) embodies it. "Fidelity" here means the gap between what a
system treats as the identity of a derived artifact and what actually determines
that artifact's value.

## Sharpening the problem

* **Write the derivation function down.** An artifact is the value of
  `f(inputs)`. The whole question of correctness is whether a system's notion of
  "the same artifact" coincides with equality of `(f, inputs)`. Most confusion
  comes from an incomplete accounting of `inputs`: upstream data, parameters,
  the code of `f`, the toolchain/environment, and any hidden state (clock,
  randomness, locale, filesystem order). Every determinant left out of the
  identity is a source of silent staleness.

* **Treat code as data.** A code dependency and a data dependency are the same
  relation at different scales. A faithful account folds the version of `f` into
  the input set, so that editing a transform invalidates its outputs exactly as
  changing a raw input would. Systems that track one but not the other are only
  half-reproducible.

* **The real design space is cache economics, not scheduling.** If everything is
  raw data plus cached computation, the open problem is *which* derivations to
  materialize and which to recompute on demand. That is an optimization over
  storage cost, recompute cost, access frequency, and tolerable latency — a
  caching and eviction question. Scheduling is downstream of it.

* **Name the addressing scheme.** Location-addressed (a path/convention),
  content-addressed (a hash of the bytes), and derivation-addressed (a hash of
  recipe + inputs) each make a different promise and fail differently. A
  system's guarantees are mostly decided by which one it uses to answer "is this
  already built?"

* **Reproducibility is a spectrum, not a bit.** Distinguish: same inputs yield
  the same bytes (bit-reproducible); yield the same value under some equivalence
  (semantically reproducible); or are merely obtainable again at all
  (recomputable). Randomness, nondeterministic tools, and floating point decide
  where a pipeline sits.

## A fidelity rubric

Questions to put to any data system; the answers place it on the spectrum above.

* **Identity completeness.** What does it hash or compare to decide "already
  built" — output existence, input mtime, or input content — and which real
  determinants (code, params, environment) are *missing* from that check? The
  missing set is the fidelity gap, and it manifests as false cache hits.
* **Immutability.** Are artifacts append-only and never mutated in place, so a
  name always denotes one value?
* **Where data lives.** Does data pass through durable, addressable storage, or
  in memory between steps? This bounds scale and decides whether intermediates
  survive a crash and can be inspected.
* **Provenance.** Given any artifact, can the system say what produced it, from
  which inputs, with which code?
* **Atomicity.** On interruption, can a partial output be mistaken for a
  complete one?
* **Cache discipline.** Can it reclaim an intermediate and rederive it on
  demand — i.e. does it truly treat manifest data as a cache, or as precious
  state it dares not delete?

## Prior art to lean on

* "Build Systems à la Carte" (Mokhov, Mitchell, Peyton Jones) already formalizes
  tasks, keys, and rebuilders, and — usefully — shows that *how to schedule* and
  *how to decide staleness* are two independent choices. Borrowing that
  vocabulary keeps this discussion rigorous instead of reinventing terms.
* Nix and content-addressed stores are the sharpest existing answer to
  "derivation as identity"; worth studying as the high-fidelity end of the
  spectrum, and for where that fidelity becomes too expensive to want.
