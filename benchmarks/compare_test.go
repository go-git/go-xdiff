package benchmarks

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aymanbagabas/go-udiff"
	"github.com/go-git/go-xdiff"
	"github.com/go-git/go-xdiff/benchmarks/internal/corpus"
	"github.com/go-git/go-xdiff/internal/testref"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// This is go-git's utils/diff.DoWithTimeout implementation at revision
// 12b788e49978b5807b748e425de6fba31af979c9, with its one-hour Do default.
func goDiff(old, new string) []diffmatchpatch.Diff {
	dmp := diffmatchpatch.New()
	dmp.DiffTimeout = time.Hour
	a, b, lines := dmp.DiffLinesToRunes(old, new)
	return dmp.DiffCharsToLines(dmp.DiffMainRunes(a, b, false), lines)
}

func lineCount(data string) int {
	if data == "" {
		return 0
	}
	n := strings.Count(data, "\n")
	if data[len(data)-1] != '\n' {
		n++
	}
	return n
}

func xdiffLines(result *xdiff.Result) int {
	n := 0
	for _, c := range result.Changes() {
		n += c.OldLines + c.NewLines
	}
	return n
}

func udiffLines(old string, edits []udiff.Edit) int {
	n := 0
	for _, e := range edits {
		n += lineCount(old[e.Start:e.End]) + lineCount(e.New)
	}
	return n
}

func dmpLines(diffs []diffmatchpatch.Diff) int {
	n := 0
	for _, d := range diffs {
		if d.Type != diffmatchpatch.DiffEqual {
			n += lineCount(d.Text)
		}
	}
	return n
}

func TestCorpus(t *testing.T) {
	var reference *testref.Oracle
	if os.Getenv("GO_XDIFF_TESTREF") == "1" {
		reference = testref.New(t)
	}
	for _, c := range corpus.Cases() {
		t.Run(c.Name, func(t *testing.T) {
			result, err := xdiff.Diff(context.Background(), c.Old, c.New, nil)
			if err != nil {
				t.Fatal(err)
			}
			var old, new []byte
			for _, chunk := range result.Chunks() {
				old = append(old, chunk.Old...)
				new = append(new, chunk.New...)
			}
			if !bytes.Equal(old, c.Old) || !bytes.Equal(new, c.New) {
				t.Fatal("go-xdiff reconstruction failed")
			}
			edits := udiff.Lines(c.OldText, c.NewText)
			patched, err := udiff.Apply(c.OldText, edits)
			if err != nil {
				t.Fatal(err)
			}
			if patched != c.NewText {
				t.Fatal("go-udiff reconstruction failed")
			}
			diffs := goDiff(c.OldText, c.NewText)
			var dmpOld, dmpNew strings.Builder
			for _, d := range diffs {
				if d.Type != diffmatchpatch.DiffInsert {
					dmpOld.WriteString(d.Text)
				}
				if d.Type != diffmatchpatch.DiffDelete {
					dmpNew.WriteString(d.Text)
				}
			}
			if dmpOld.String() != c.OldText || dmpNew.String() != c.NewText {
				t.Fatal("go-diff reconstruction failed")
			}
			if reference != nil {
				want, got := reference.Script(t, c.Old, c.New, 0), result.Changes()
				if len(want) != len(got) {
					t.Fatalf("Git C script length: %d != %d", len(want), len(got))
				}
				for i, w := range want {
					if got[i] != (xdiff.Change{OldStart: w.OldStart, OldLines: w.OldCount,
						NewStart: w.NewStart, NewLines: w.NewCount}) {
						t.Fatalf("Git C script mismatch at change %d", i)
					}
				}
			}
			t.Logf("%d+%d bytes; changed lines: go-xdiff=%d go-udiff=%d go-diff=%d",
				len(c.Old), len(c.New), xdiffLines(result), udiffLines(c.OldText, edits), dmpLines(diffs))
		})
	}
}

var (
	xdiffSink *xdiff.Result
	udiffSink []udiff.Edit
	dmpSink   []diffmatchpatch.Diff
)

func BenchmarkCompare(b *testing.B) {
	for _, c := range corpus.Cases() {
		b.Run(c.Name, func(b *testing.B) {
			totalBytes := int64(len(c.Old) + len(c.New))
			b.Run("GoXDiff", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(totalBytes)
				result, err := xdiff.Diff(context.Background(), c.Old, c.New, nil)
				if err != nil {
					b.Fatal(err)
				}
				changed := xdiffLines(result)
				for b.Loop() {
					result, err = xdiff.Diff(context.Background(), c.Old, c.New, nil)
					if err != nil {
						b.Fatal(err)
					}
				}
				xdiffSink = result
				b.ReportMetric(float64(changed), "edit-lines/op")
			})
			b.Run("GoUDiff", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(totalBytes)
				edits := udiff.Lines(c.OldText, c.NewText)
				changed := udiffLines(c.OldText, edits)
				for b.Loop() {
					edits = udiff.Lines(c.OldText, c.NewText)
				}
				udiffSink = edits
				b.ReportMetric(float64(changed), "edit-lines/op")
			})
			b.Run("GoDiff", func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(totalBytes)
				diffs := goDiff(c.OldText, c.NewText)
				changed := dmpLines(diffs)
				for b.Loop() {
					diffs = goDiff(c.OldText, c.NewText)
				}
				dmpSink = diffs
				b.ReportMetric(float64(changed), "edit-lines/op")
			})
		})
	}
}

// This includes the string-to-byte copies required by go-git's current inputs.
func BenchmarkStringAdapter(b *testing.B) {
	for _, c := range corpus.Cases() {
		b.Run(c.Name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(c.Old) + len(c.New)))
			var result *xdiff.Result
			var err error
			for b.Loop() {
				result, err = xdiff.Diff(context.Background(), []byte(c.OldText), []byte(c.NewText), nil)
				if err != nil {
					b.Fatal(err)
				}
			}
			xdiffSink = result
		})
	}
}

func Example_goDiff() {
	for _, d := range goDiff("one\ntwo\n", "one\nthree\n") {
		fmt.Printf("%d %q\n", d.Type, d.Text)
	}
	// Output:
	// 0 "one\n"
	// -1 "two\n"
	// 1 "three\n"
}
