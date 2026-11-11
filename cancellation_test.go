package xdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
)

func checkCancellation(t *testing.T, run func(context.Context) error) {
	t.Helper()
	const unlimited = int(^uint(0) >> 1)
	ctx := &algorithmCancelContext{Context: t.Context(), remaining: unlimited}
	if err := run(ctx); err != nil {
		t.Fatalf("uncanceled operation: %v", err)
	}
	checks := unlimited - ctx.remaining
	if checks == 0 {
		t.Fatal("operation did not check its context")
	}
	for cancelAt := 1; cancelAt <= checks; cancelAt++ {
		ctx := &algorithmCancelContext{Context: t.Context(), remaining: cancelAt}
		if err := run(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation at check %d of %d: %v", cancelAt, checks, err)
		}
	}
}

func TestDiffCancellationAtEveryCheck(t *testing.T) {
	for _, pair := range []struct{ name, old, new string }{
		{"unrelated", "old\nold\n", "new\nnew\n"},
		{"repeated", "a\nb\na\nb\n", "b\na\nb\na\n"},
		{"anchors", "start\na\nshared\nb\nend\n", "start\nb\nshared\na\nend\n"},
		{"blank-boundary", "head\na\n\nb\ntail\n", "head\na\n\nb\na\n\nb\ntail\n"},
		{"changed-ends", "old\nsame\nold\n", "new\nsame\nnew\n"},
		{"recursive-anchors", "start\nold\nanchor-one\nanchor-two\nend\n", "start\nnew\nanchor-one\nanchor-two\nend\n"},
		{"repeated-replacement", "a\na\na\n", "b\na\na\n"},
		{"repeated-common-tail", "old\nanchor\nsame\nsame\n", "new\nanchor\nsame\nsame\n"},
		{"identical", "same\nsame\n", "same\nsame\n"},
		{"insert", "", "new\nnew"},
		{"delete", "old\nold", ""},
		{"empty", "", ""},
	} {
		for _, algorithm := range []Algorithm{Myers, Patience, Histogram} {
			t.Run(fmt.Sprintf("%s/%d", pair.name, algorithm), func(t *testing.T) {
				opts := Options{Algorithm: algorithm, IndentHeuristic: true}
				if algorithm == Patience {
					opts.Anchors = []string{"absent", "shared"}
				}
				old, new := []byte(pair.old), []byte(pair.new)
				checkCancellation(t, func(ctx context.Context) error {
					result, err := Diff(ctx, old, new, &opts)
					if err != nil && result != nil {
						t.Fatal("canceled diff returned a result")
					}
					if err == nil {
						checkChunks(t, old, new, result)
					}
					return err
				})
			})
		}
	}
}

func TestHunkCancellationAtEveryCheck(t *testing.T) {
	for _, pair := range []struct{ name, old, new string }{
		{"functions", "first\n old\n}\n\nsecond\n old\n}\n", "first\n new\n}\n\nsecond\n new\n}\n"},
		{"ignored", "\nkeep\n\nkeep\nold\n", "keep\nkeep\nnew\n"},
		{"appended", "first\n body\n}\n", "first\n body\n}\n\nsecond\n body\n}\n"},
	} {
		r, err := Diff(t.Context(), []byte(pair.old), []byte(pair.new), &Options{IgnoreBlankLines: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, emit := range []EmitOptions{
			{}, {Context: 1}, {Context: 3, InterHunkContext: 2},
			{FunctionNames: true, FunctionContext: true},
		} {
			t.Run(fmt.Sprintf("%s/%+v", pair.name, emit), func(t *testing.T) {
				checkCancellation(t, func(ctx context.Context) error {
					hunks, err := r.Hunks(ctx, &emit)
					if err != nil && hunks != nil {
						t.Fatal("canceled hunk construction returned hunks")
					}
					return err
				})
				checkCancellation(t, func(ctx context.Context) error {
					return WriteUnified(ctx, io.Discard, r, &emit)
				})
				for _, customHeader := range []bool{false, true} {
					callbacks := EmitCallbacks{LineFunc: func(...[]byte) error { return nil }}
					if customHeader {
						callbacks.HunkHeaderFunc = func(Hunk) error { return nil }
					}
					checkCancellation(t, func(ctx context.Context) error {
						return r.Emit(ctx, &emit, &callbacks)
					})
				}
				rangeCallbacks := EmitCallbacks{HunkFunc: func(Change) error { return nil }}
				checkCancellation(t, func(ctx context.Context) error {
					return r.Emit(ctx, &emit, &rangeCallbacks)
				})
				checkCancellation(t, func(ctx context.Context) error {
					ranges, err := r.HunkRanges(ctx, &emit)
					if err != nil && ranges != nil {
						t.Fatal("canceled range collection returned ranges")
					}
					return err
				})
			})
		}
	}
}

func TestMergeCancellationAtEveryCheck(t *testing.T) {
	for _, triple := range []struct{ name, base, ours, theirs string }{
		{"unchanged", "base\n", "base\n", "base\n"},
		{"one-sided", "base\n", "base\n", "changed\n"},
		{"independent", "one\ntwo\nthree\nfour\nfive\n", "ONE\ntwo\nthree\nfour\nFIVE\n", "one\ntwo\nTHREE\nfour\nfive\n"},
		{"independent-reversed", "one\ntwo\nthree\nfour\nfive\n", "one\ntwo\nTHREE\nfour\nfive\n", "ONE\ntwo\nthree\nfour\nFIVE\n"},
		{"identical", "base\n", "same\n", "same\n"},
		{"refined", "base\n", "head\nours\n!\n;\n(\n)\nours-two\ntail\n", "head\ntheirs\n!\n;\n(\n)\ntheirs-two\ntail\n"},
		{"missing-newline", "base\r\n", "ours", "theirs"},
	} {
		for _, style := range []ConflictStyle{ConflictMerge, ConflictDiff3, ConflictZDiff3} {
			for _, favor := range []MergeFavor{FavorConflict, FavorOurs, FavorTheirs, FavorUnion} {
				t.Run(fmt.Sprintf("%s/%d/%d", triple.name, style, favor), func(t *testing.T) {
					opts := MergeOptions{Level: MergeZealousAlnum, Style: style, Favor: favor, MarkerSize: 257,
						AncestorLabel: "base", OursLabel: "ours", TheirsLabel: "theirs"}
					checkCancellation(t, func(ctx context.Context) error {
						result, err := Merge(ctx, []byte(triple.base), []byte(triple.ours), []byte(triple.theirs), &opts)
						if err != nil && result != nil {
							t.Fatal("canceled merge returned a result")
						}
						return err
					})
				})
			}
		}
	}
	t.Run("large-label", func(t *testing.T) {
		checkCancellation(t, func(ctx context.Context) error {
			output := mergeOutput{ctx: ctx}
			return output.marker('<', 257, strings.Repeat("label", 14000), true)
		})
	})
}

func TestMyersLongSnakeCancellation(t *testing.T) {
	common := make([]int, 2048)
	for i := range common {
		common[i] = 1
	}
	for _, pair := range []struct {
		name string
		a, b []int
	}{
		{"prefix", append(slices.Clone(common), 2), append(slices.Clone(common), 3)},
		{"suffix", append([]int{2}, common...), append([]int{3}, common...)},
		{"internal", append(slices.Clone(common), 2), append([]int{3}, common...)},
	} {
		t.Run(pair.name, func(t *testing.T) {
			checkCancellation(t, func(ctx context.Context) error {
				e := environment{a: myersTestFile(pair.a), b: myersTestFile(pair.b), ctx: ctx}
				size := len(pair.a) + len(pair.b) + 3
				s := myersState{e: &e, forward: make([]int, size), backward: make([]int, size),
					base: len(pair.b) + 1, maxCost: myersMaxCostMin}
				return s.compare(0, len(pair.a), 0, len(pair.b), false)
			})
		})
	}
}

func TestCompactRediffCancellation(t *testing.T) {
	var classes classifier
	a, err := readFile(t.Context(), []byte("a\na\na\n"), 0, &classes)
	if err != nil {
		t.Fatal(err)
	}
	b, err := readFile(t.Context(), []byte("x\na\ny\n"), 0, &classes)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []*file{&a, &b} {
		f.setChanged(0, true)
		f.setChanged(2, true)
	}
	checkCancellation(t, func(ctx context.Context) error {
		e := environment{a: a, b: b, ctx: ctx, opts: Options{Algorithm: Histogram}}
		e.a.changed, e.b.changed = slices.Clone(a.changed), slices.Clone(b.changed)
		return e.compact()
	})
}

type writeFunc func([]byte) (int, error)

func (f writeFunc) Write(p []byte) (int, error) { return f(p) }

func TestOutputFailureAtEveryWrite(t *testing.T) {
	r, err := Diff(t.Context(), []byte("head\nold"), []byte("head\nnew"), nil)
	if err != nil {
		t.Fatal(err)
	}
	opts := EmitOptions{Context: 1}
	calls := 0
	var want bytes.Buffer
	if err := WriteUnified(t.Context(), writeFunc(func(p []byte) (int, error) {
		calls++
		return want.Write(p)
	}), r, &opts); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("write failure")
	for _, mode := range []string{"error", "short", "cancel"} {
		for failAt := 1; failAt <= calls; failAt++ {
			ctx, cancel := context.WithCancel(t.Context())
			var got bytes.Buffer
			remaining := failAt
			err := WriteUnified(ctx, writeFunc(func(p []byte) (int, error) {
				remaining--
				if remaining == 0 {
					switch mode {
					case "short":
						return got.Write(p[:len(p)-1])
					case "cancel":
						cancel()
						return got.Write(p)
					}
					return 0, failure
				}
				return got.Write(p)
			}), r, &opts)
			cancel()
			expected := failure
			switch mode {
			case "short":
				expected = io.ErrShortWrite
			case "cancel":
				expected = context.Canceled
			}
			if !errors.Is(err, expected) {
				t.Fatalf("write %d, mode=%s: %v, want %v", failAt, mode, err, expected)
			}
			if remaining != 0 {
				t.Fatalf("write %d, mode=%s: output continued after failure", failAt, mode)
			}
			if !bytes.HasPrefix(want.Bytes(), got.Bytes()) || (mode != "cancel" && got.Len() == want.Len()) {
				t.Fatalf("write %d, mode=%s: unexpected output prefix", failAt, mode)
			}
		}
	}
}
