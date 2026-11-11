# go-xdiff

A pure-Go port of Git's xdiff for line diffs and three-way merges.
It supports Myers, minimal, patience, anchored, and histogram diffs.
It requires Go 1.26 or later, with no external dependencies, CGO, or Git executable.

## Install

```sh
go get github.com/go-git/go-xdiff
```

## Diff

```go
package main

import (
	"context"
	"log"
	"os"

	"github.com/go-git/go-xdiff"
)

func main() {
	ctx := context.Background()
	result, err := xdiff.Diff(ctx, []byte("one\ntwo\n"), []byte("one\nthree\n"), nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := xdiff.WriteUnified(ctx, os.Stdout, result, &xdiff.EmitOptions{Context: 3}); err != nil {
		log.Fatal(err)
	}
}
```

Use `Options` to select an algorithm, ignore whitespace, or enable the indent heuristic.
Nil options select Myers without the indent heuristic.

The result provides:

- `Changes()` for replacements with zero-based line offsets.
- `Chunks()` for equal, delete, and insert byte ranges.
- `Hunks()` for structured unified hunks.
- `Emit()` for callback-based unified output without file headers.

Use the package-level `WriteUnified()` helper for output to an `io.Writer`.
For custom output, pass `EmitCallbacks` to `Emit()`.
`LineFunc` receives separate prefix, content, and optional missing-newline notice parts.
It receives each standard hunk header as one part.
The parts and their bytes are borrowed. Do not modify or retain them.

An optional `HunkHeaderFunc` replaces the standard header and receives a `Hunk` with zero-based ranges.
`NoHunkHeader` suppresses both header paths.
`LineFunc` is required, even when there are no changes, unless `EmitOptions.HunkFunc` is set.
Callbacks return errors to stop emission. `Emit()` checks cancellation before and after each callback.
The caller controls colors and terminal detection.

Set `EmitOptions.HunkFunc` for range-only output without header or line callbacks.
Context settings control grouping, but returned ranges exclude outer context and function expansion.
`Hunks()` ignores `HunkFunc` and still returns complete hunks.

Use `HunkRanges()` to collect the same ranges into a slice.
It reuses `EmitOptions` but does not call `HunkFunc` or `FindFunc`.

```go
ranges, err := result.HunkRanges(ctx, &xdiff.EmitOptions{Context: 3})
if err != nil {
	return err
}
```

## Merge

```go
result, err := xdiff.Merge(ctx, base, ours, theirs, nil)
if err != nil {
	return err
}
// result.Content contains the merged bytes, including any conflict markers.
// result.Conflicts counts unresolved conflicts.
```

Conflicts are results, not errors.
Nil options select zealous refinement.
An explicit zero-valued `MergeOptions` selects minimal refinement.
Use options to select diff3 or zdiff3 markers, labels, or automatic conflict resolution.

## Examples

Run these examples from the repository root:

```sh
go run ./examples/diff       # Unified diff output.
go run ./examples/algorithms # Algorithm selection and zero-based line ranges.
go run ./examples/merge      # Clean merges and diff3 conflict markers.
```

## Behavior

Inputs are `[]byte`. Lines end at LF bytes.
The library preserves CRLF, arbitrary bytes, and missing final newlines.
It does not detect binary files.

Diff results reference the inputs. Do not modify input bytes while using a result.
Options remain unchanged. Calls support context cancellation.
Equal chunks retain both inputs when whitespace rules match different bytes.
Blank-line and regex filters affect hunks and output, not changes or chunks.
Ignore expressions use Go's `regexp` rules; use `(?m)` for line anchors.
Use `regexp.CompilePOSIX` for POSIX ERE syntax and newline-aware matching.
Exact C regex compatibility is not guaranteed: Go matches UTF-8 code points, while C character interpretation and classes depend on the locale.

See the [API documentation](https://pkg.go.dev/github.com/go-git/go-xdiff),
[API examples](example_test.go), and [benchmarks](benchmarks/README.md).

## Development

```sh
make validate test
make reference # Requires a C compiler.
```

The [C reference](internal/testref/README.md) supplies comparison tests.
A scheduled workflow proposes upstream updates through pull requests.

## License

[LGPL-2.1-or-later](LICENSE), with the histogram algorithm's EDL-1.0 notice.
See [NOTICE](NOTICE) and the source headers.
Distributors must comply with the applicable LGPL source, notice, and relinking requirements.
