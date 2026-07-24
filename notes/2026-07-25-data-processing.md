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
