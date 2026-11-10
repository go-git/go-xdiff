# C reference tests

This package builds Git's xdiff as a standalone test executable, without CGO.
It compares diff scripts, unified output, merged bytes, and conflict counts.
Normal library tests do not require a C compiler.

## Run

From the repository root:

```sh
make reference
```

Requires a POSIX-style C compiler (`cc`) on Linux or macOS.
Keep the source files available at their checkout paths; do not use Go's `-trimpath`.
See [oracle.go](oracle.go) for the test helper API.

## Source and updates

[`upstream/REVISION`](upstream/REVISION) identifies the vendored Git commit.
C sources retain their original license notices; see [LICENSE](../../LICENSE) and [NOTICE](../../NOTICE).
The local runner and compatibility header are separate from the upstream sources.

The **Update C reference** workflow runs weekly or manually from the Actions tab.
It updates the revision and opens a PR only when C sources or headers change.
It leaves the Go implementation unchanged and includes reference-check results.
Review failed comparisons before merging.

Enable **Allow GitHub Actions to create and approve pull requests** in the repository's Actions settings.
