# Native Git xdiff benchmark

The [comparison script](../run.sh) builds and runs this C runner.
It uses the vendored [Git revision](../../internal/testref/upstream/REVISION).

Timing includes preparation, default Myers, compaction, script construction, and cleanup.
It excludes compilation, process startup, file reads, and warmup.
The runner does not measure allocations or produce unified output.

See the [benchmark guide](../README.md) to run the comparison.
