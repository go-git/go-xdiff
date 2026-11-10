package xdiff

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

func benchmarkInputs() ([]byte, []byte, []byte) {
	var base, ours, theirs bytes.Buffer
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&base, "line %d: unchanged text\n", i)
		switch {
		case i%97 == 0:
			fmt.Fprintf(&ours, "line %d: our edit\n", i)
		default:
			fmt.Fprintf(&ours, "line %d: unchanged text\n", i)
		}
		switch {
		case i%101 == 0:
			fmt.Fprintf(&theirs, "line %d: their edit\n", i)
		default:
			fmt.Fprintf(&theirs, "line %d: unchanged text\n", i)
		}
	}
	return base.Bytes(), ours.Bytes(), theirs.Bytes()
}

func BenchmarkDiff(b *testing.B) {
	old, new, _ := benchmarkInputs()
	for _, spec := range []struct {
		name string
		opts Options
	}{
		{"Myers", Options{}},
		{"Minimal", Options{Minimal: true}},
		{"Patience", Options{Algorithm: Patience}},
		{"Histogram", Options{Algorithm: Histogram}},
	} {
		b.Run(spec.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(old) + len(new)))
			for b.Loop() {
				if _, err := Diff(context.Background(), old, new, &spec.opts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDiffRepetitive(b *testing.B) {
	old := bytes.Repeat([]byte("a\nb\nc\n"), 400)
	new := bytes.Repeat([]byte("b\nc\na\n"), 400)
	for _, algorithm := range []Algorithm{Myers, Patience, Histogram} {
		b.Run(fmt.Sprint(algorithm), func(b *testing.B) {
			b.ReportAllocs()
			opts := Options{Algorithm: algorithm, IndentHeuristic: true}
			for b.Loop() {
				if _, err := Diff(context.Background(), old, new, &opts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkMerge(b *testing.B) {
	base, ours, theirs := benchmarkInputs()
	b.ReportAllocs()
	b.SetBytes(int64(len(base) + len(ours) + len(theirs)))
	for b.Loop() {
		if _, err := Merge(context.Background(), base, ours, theirs, nil); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkDiffWhitespace(b *testing.B) {
	old, new, _ := benchmarkInputs()
	for _, mode := range []Whitespace{IgnoreAllSpace, IgnoreSpaceChange, IgnoreSpaceAtEOL, IgnoreCRAtEOL} {
		b.Run(fmt.Sprint(mode), func(b *testing.B) {
			b.ReportAllocs()
			opts := Options{Whitespace: mode}
			for b.Loop() {
				if _, err := Diff(b.Context(), old, new, &opts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDiffSpecialInputs(b *testing.B) {
	old, _, _ := benchmarkInputs()
	for _, pair := range []struct {
		name     string
		old, new []byte
	}{
		{"Identical", old, bytes.Clone(old)},
		{"Insert", nil, old},
		{"Delete", old, nil},
		{"Empty", nil, nil},
	} {
		for _, algorithm := range []Algorithm{Myers, Patience, Histogram} {
			b.Run(fmt.Sprintf("%s/%d", pair.name, algorithm), func(b *testing.B) {
				b.ReportAllocs()
				opts := Options{Algorithm: algorithm}
				for b.Loop() {
					if _, err := Diff(b.Context(), pair.old, pair.new, &opts); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

func BenchmarkChunks(b *testing.B) {
	old, new, _ := benchmarkInputs()
	result, err := Diff(b.Context(), old, new, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		result.Chunks()
	}
}
