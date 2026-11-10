# Benchmarks

Compare go-xdiff with native Git xdiff, go-udiff, and go-diff.
This separate module keeps competitor dependencies out of the library.
Versions come from [go.mod](go.mod) and the [C revision](../internal/testref/upstream/REVISION).

## Run

From the repository root:

```sh
make compare
```

Requires Go, Bash, a C compiler, and `sha256sum` or `shasum`.
Results go to `benchmarks/.results/summary.md`.
The same directory contains raw samples, environment metadata, source hashes, and validation output.
Results stay local and are not checked in.

To change the sample count, duration, or output directory:

```sh
COUNT=5 DURATION_NS=300000000 bash benchmarks/run.sh /path/to/results
```

## Scope

All engines compute line diffs on the same inputs.
Native inputs are prepared before timing; a separate benchmark includes string-to-byte conversions.
C timings exclude compilation, process startup, and file reads.
C allocation counts are not measured.

The suite checks reconstruction and exact go-xdiff/Git C scripts before timing.
Compare edit counts as well as speed: a coarser diff can be faster.
The report uses sample medians, with engines run sequentially.

Run `make benchmark` for the library's algorithm and merge benchmarks.
