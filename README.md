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
	if err := result.WriteUnified(ctx, os.Stdout, &xdiff.EmitOptions{Context: 3}); err != nil {
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
- `WriteUnified()` for unified output without file headers.

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

## Behavior

Inputs are `[]byte`. Lines end at LF bytes.
The library preserves CRLF, arbitrary bytes, and missing final newlines.
It does not detect binary files.

Diff results reference the inputs. Do not modify input bytes while using a result.
Options remain unchanged. Calls support context cancellation.
Equal chunks retain both inputs when whitespace rules match different bytes.
Blank-line and regex filters affect hunks and output, not changes or chunks.
Ignore expressions use Go's `regexp` rules; use `(?m)` for line anchors.

See the [API documentation](https://pkg.go.dev/github.com/go-git/go-xdiff),
[examples](example_test.go), and [benchmarks](benchmarks/README.md).

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
