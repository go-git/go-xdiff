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

func BenchmarkOutput(b *testing.B) {
	old, new, _ := benchmarkInputs()
	for _, pair := range []struct {
		name     string
		old, new []byte
	}{
		{"Sparse", old, new},
		{"LargeHunk", bytes.Repeat([]byte("old\n"), 2000), bytes.Repeat([]byte("new\n"), 2000)},
		{"Unchanged", old, bytes.Clone(old)},
	} {
		b.Run(pair.name, func(b *testing.B) {
			result, err := Diff(b.Context(), pair.old, pair.new, nil)
			if err != nil {
				b.Fatal(err)
			}
			b.Run("Hunks", func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					if _, err := result.Hunks(b.Context(), &EmitOptions{Context: 3}); err != nil {
						b.Fatal(err)
					}
				}
			})
			for _, spec := range []struct {
				name     string
				noHeader bool
				header   func(Hunk) error
			}{
				{"Emit", false, nil},
				{"NoHeader", true, nil},
				{"CustomHeader", false, func(Hunk) error { return nil }},
			} {
				b.Run(spec.name, func(b *testing.B) {
					opts := EmitOptions{Context: 3, NoHunkHeader: spec.noHeader}
					callbacks := EmitCallbacks{
						HunkHeaderFunc: spec.header,
						LineFunc:       func(...[]byte) error { return nil },
					}
					b.ReportAllocs()
					for b.Loop() {
						if err := result.Emit(b.Context(), &opts, &callbacks); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

func BenchmarkMergeSpecialInputs(b *testing.B) {
	base, ours, theirs := benchmarkInputs()
	for _, pair := range []struct {
		name         string
		ours, theirs []byte
	}{
		{"Unchanged", bytes.Clone(base), bytes.Clone(base)},
		{"OursUnchanged", bytes.Clone(base), theirs},
		{"TheirsUnchanged", ours, bytes.Clone(base)},
		{"SameEdits", ours, bytes.Clone(ours)},
	} {
		b.Run(pair.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(base) + len(pair.ours) + len(pair.theirs)))
			for b.Loop() {
				if _, err := Merge(b.Context(), base, pair.ours, pair.theirs, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkMergeWhitespace(b *testing.B) {
	base := bytes.Repeat([]byte("original\n"), 2000)
	ours := bytes.Repeat([]byte("one two\n"), 2000)
	for _, spec := range []struct {
		name string
		mode Whitespace
		line string
	}{
		{"All", IgnoreAllSpace, "o ne  t w o\n"},
		{"Change", IgnoreSpaceChange, "one  two\n"},
		{"EOL", IgnoreSpaceAtEOL, "one two \t\n"},
		{"CR", IgnoreCRAtEOL, "one two\r\n"},
	} {
		b.Run(spec.name, func(b *testing.B) {
			theirs := bytes.Repeat([]byte(spec.line), 2000)
			opts := MergeOptions{Level: MergeEager, Diff: Options{Whitespace: spec.mode}}
			b.ReportAllocs()
			b.SetBytes(int64(len(base) + len(ours) + len(theirs)))
			for b.Loop() {
				if _, err := Merge(b.Context(), base, ours, theirs, &opts); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
