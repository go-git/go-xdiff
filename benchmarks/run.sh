#!/usr/bin/env bash
set -euo pipefail

cd -- "$(dirname -- "$0")"
ROOT=$(cd .. && pwd)
OUT=${1:-"$PWD/.results"}
COUNT=${COUNT:-5}
DURATION_NS=${DURATION_NS:-300000000}
CC=${CC:-cc}

if [[ $# -gt 1 || ! "$COUNT" =~ ^[1-9][0-9]*$ || ! "$DURATION_NS" =~ ^[1-9][0-9]*$ ]]; then
    echo "usage: COUNT=5 DURATION_NS=300000000 bash benchmarks/run.sh [OUTPUT_DIRECTORY]" >&2
    exit 1
fi

mkdir -p -- "$OUT"
OUT=$(cd -- "$OUT" && pwd)
export GOMAXPROCS=1

{
    date -u '+UTC: %Y-%m-%dT%H:%M:%SZ'
    go version
    go env GOOS GOARCH
    "$CC" --version | head -n 1
    printf 'Samples: %s\nTarget duration per sample: %s ns\nGOMAXPROCS: 1\n' "$COUNT" "$DURATION_NS"
    printf 'C flags: -std=c99 -O2\n'
    printf 'Git C: %s\n' "$(cat "$ROOT/internal/testref/upstream/REVISION")"
    printf 'go-git wrapper: 12b788e49978b5807b748e425de6fba31af979c9\n'
    go list -m all
} > "$OUT/metadata.txt"

sources=(
    "$ROOT"/*.go
    "$ROOT"/internal/testref/upstream/*.[ch]
    "$ROOT/internal/testref/upstream/REVISION"
    "$ROOT/internal/testref/runner/git-compat-util.h"
    compare_test.go internal/corpus/corpus.go native/runner.c go.mod go.sum
)
if command -v sha256sum > /dev/null; then
    sha256sum "${sources[@]}" > "$OUT/source-sha256.txt"
elif command -v shasum > /dev/null; then
    shasum -a 256 "${sources[@]}" > "$OUT/source-sha256.txt"
else
    echo "source fingerprints require sha256sum or shasum" >&2
    exit 1
fi

go run ./cmd/fixtures -dir "$OUT/fixtures"
"$CC" -std=c99 -O2 \
    -I "$ROOT/internal/testref/runner" -I "$ROOT/internal/testref/upstream" \
    native/runner.c "$ROOT"/internal/testref/upstream/*.c \
    -o "$OUT/git-xdiff-bench"

GO_XDIFF_TESTREF=1 go test -count=1 -v ./... | tee "$OUT/validation.txt"
go test -run '^$' -bench '^(BenchmarkCompare|BenchmarkStringAdapter)$' \
    -benchmem -count "$COUNT" -benchtime "${DURATION_NS}ns" -cpu 1 . | tee "$OUT/go.txt"

: > "$OUT/c.txt"
for name in Unchanged SmallEdit LargeEdit Scattered Repeated Unrelated Unicode LongLines; do
    "$OUT/git-xdiff-bench" "$name" "$OUT/fixtures/$name.old" "$OUT/fixtures/$name.new" \
        "$DURATION_NS" "$COUNT" | tee -a "$OUT/c.txt"
done
go run ./cmd/report -dir "$OUT" -count "$COUNT" | tee "$OUT/summary.md"
