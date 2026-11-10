package xdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"reflect"
	"regexp"
	"testing"

	"github.com/go-git/go-xdiff/internal/testref"
)

func checkChunks(t testing.TB, a, b []byte, r *Result) {
	t.Helper()
	var old, new []byte
	for _, chunk := range r.Chunks() {
		old = append(old, chunk.Old...)
		new = append(new, chunk.New...)
		if chunk.Operation == Equal && r.opts.Whitespace == 0 && !bytes.Equal(chunk.Old, chunk.New) {
			t.Fatalf("unequal equal chunk: %q / %q", chunk.Old, chunk.New)
		}
	}
	if !bytes.Equal(old, a) || !bytes.Equal(new, b) {
		t.Fatalf("reconstruction failed: old %q != %q; new %q != %q", old, a, new, b)
	}
}

func TestDiff(t *testing.T) {
	for _, pair := range [][2]string{
		{"", ""}, {"", "\n"}, {"a", "a\n"}, {"a\n", "a"}, {"a\nb\nc\n", "a\nB\nc\n"},
		{"x\nx\na\n", "x\na\nx\n"}, {"\xff\x00\n", "\xff\x00\nz"}, {"a\r\n", "a\n"},
	} {
		t.Run(fmt.Sprintf("%q/%q", pair[0], pair[1]), func(t *testing.T) {
			a, b := []byte(pair[0]), []byte(pair[1])
			r, err := Diff(context.Background(), a, b, nil)
			if err != nil {
				t.Fatal(err)
			}
			checkChunks(t, a, b, r)
		})
	}
}

func TestDiffRecordRegions(t *testing.T) {
	var old []byte
	for i := 0; i < 16; i++ {
		old = fmt.Appendf(old, "line %d\n", i)
	}
	old = append(old, "\x00\xff\n\ntail"...)
	new := bytes.Replace(old, []byte("\x00\xff"), []byte("\x00\xfe"), 1)
	copy(new[len(new)-4:], "TAIL")
	want := []Change{{16, 1, 16, 1}, {18, 1, 18, 1}}
	for _, algorithm := range []Algorithm{Myers, Patience, Histogram} {
		t.Run(fmt.Sprint(algorithm), func(t *testing.T) {
			r, err := Diff(context.Background(), old, new, &Options{Algorithm: algorithm})
			if err != nil {
				t.Fatal(err)
			}
			checkChunks(t, old, new, r)
			if !reflect.DeepEqual(r.Changes(), want) {
				t.Fatalf("changes: %v, want %v", r.Changes(), want)
			}
			hunks, err := r.Hunks(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(hunks) != 2 || len(hunks[0].Lines) != 2 || len(hunks[1].Lines) != 2 {
				t.Fatalf("unexpected hunks: %+v", hunks)
			}
			if !bytes.Equal(hunks[0].Lines[0].Content, []byte("\x00\xff\n")) ||
				!bytes.Equal(hunks[1].Lines[1].Content, []byte("TAIL")) {
				t.Fatal("record contents differ")
			}
			if &hunks[0].Lines[0].Content[0] != &old[bytes.IndexByte(old, 0)] ||
				&hunks[1].Lines[1].Content[0] != &new[len(new)-4] {
				t.Fatal("record contents do not refer to inputs")
			}
		})
	}
}

func TestClassifierCollisions(t *testing.T) {
	c := classifier{buckets: make([]int, 16)}
	for _, tc := range []struct {
		key  string
		want int
	}{
		{"first\n", 0}, {"\x00\xff\n", 1}, {"third", 2},
		{"first\n", 0}, {"third", 2}, {"\x00\xff\n", 1},
	} {
		if got := c.intern(7, []byte(tc.key), 3, false); got != tc.want {
			t.Fatalf("class for %q = %d, want %d", tc.key, got, tc.want)
		}
	}
	for i := 0; i < 64; i++ {
		key := fmt.Appendf(nil, "collision-%d\n", i)
		if got := c.intern(7, key, 3, false); got != i+3 {
			t.Fatalf("new collision %d: class %d", i, got)
		}
	}
	for i := 63; i >= 0; i-- {
		key := fmt.Appendf(nil, "collision-%d\n", i)
		if got := c.intern(7, key, 3, false); got != i+3 {
			t.Fatalf("existing collision %d: class %d", i, got)
		}
	}
}

func TestDiffReleasesScratch(t *testing.T) {
	for _, algorithm := range []Algorithm{Myers, Patience, Histogram} {
		for _, pair := range [][2]string{
			{"a\nb\na\n", "a\nc\na\n"}, {"same\nsame", "same\nsame"},
			{"", "a\nb"}, {"a\nb", ""}, {"", ""},
		} {
			a, b := []byte(pair[0]), []byte(pair[1])
			r, err := Diff(t.Context(), a, b, &Options{Algorithm: algorithm})
			if err != nil {
				t.Fatal(err)
			}
			if r.a.changed != nil || r.b.changed != nil || r.a.refs != nil || r.b.refs != nil {
				t.Fatal("result retained algorithm scratch")
			}
			checkChunks(t, a, b, r)
			if _, err := r.Hunks(t.Context(), &EmitOptions{Context: 1}); err != nil {
				t.Fatal(err)
			}
			if err := r.WriteUnified(t.Context(), io.Discard, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestWhitespaceClassReuse(t *testing.T) {
	lines := []string{" a \n", "\ta\t\n", "a\n", "a", "a\r\n", "a\vb\n", "ab\n"}
	for mode := Whitespace(0); mode < 16; mode++ {
		want := []int{0, 1, 2, 3, 4, 5, 6}
		switch {
		case mode&IgnoreAllSpace != 0:
			want = []int{0, 0, 0, 0, 0, 1, 2}
		case mode&IgnoreSpaceChange != 0:
			want = []int{0, 0, 1, 1, 1, 2, 3}
		case mode != 0:
			want = []int{0, 1, 2, 2, 2, 3, 4}
		}
		var classes classifier
		for i, line := range lines {
			f, err := readFile(t.Context(), []byte(line), mode, &classes)
			if err != nil {
				t.Fatal(err)
			}
			if f.recs[0].id != want[i] {
				t.Fatalf("mode %d, line %q: class %d, want %d", mode, line, f.recs[0].id, want[i])
			}
		}
	}
}

func TestNormalizedClassBlocks(t *testing.T) {
	var old, new bytes.Buffer
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&old, "line %04d repeated content\n", i)
	}
	for i := 299; i >= 0; i-- {
		fmt.Fprintf(&new, "line  %04d  repeated\tcontent \r\n", i)
	}
	for _, mode := range []Whitespace{IgnoreAllSpace, IgnoreSpaceChange} {
		var classes classifier
		a, err := readFile(t.Context(), old.Bytes(), mode, &classes)
		if err != nil {
			t.Fatal(err)
		}
		b, err := readFile(t.Context(), new.Bytes(), mode, &classes)
		if err != nil {
			t.Fatal(err)
		}
		for i := range a.recs {
			if a.recs[i].id != i || b.recs[299-i].id != i {
				t.Fatalf("mode %d, line %d: class keys did not survive block reuse", mode, i)
			}
		}
	}
}

func TestOptionsAndCancellation(t *testing.T) {
	options := Options{Anchors: []string{"a"}, IgnoreRegex: []*regexp.Regexp{regexp.MustCompile("x")}}
	before := options
	r, err := Diff(context.Background(), []byte("a\n"), []byte("b\n"), &options)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, options) {
		t.Fatal("options changed")
	}
	options.Anchors[0] = "changed"
	options.IgnoreRegex[0] = regexp.MustCompile("y")
	if r.opts.Anchors[0] != "a" || r.opts.IgnoreRegex[0].String() != "x" {
		t.Fatal("result retained caller-owned option slices")
	}
	changes := r.Changes()
	changes[0].OldStart = 100
	if r.Changes()[0].OldStart != 0 {
		t.Fatal("Changes exposes internal state")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Diff(ctx, nil, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	for _, opts := range []Options{
		{Algorithm: 99}, {Whitespace: 128}, {Algorithm: Patience, Minimal: true},
		{IgnoreRegex: []*regexp.Regexp{nil}},
	} {
		if _, err := Diff(context.Background(), nil, nil, &opts); err == nil {
			t.Fatalf("accepted invalid options: %+v", opts)
		}
	}
	if _, err := Diff(nil, nil, nil, nil); err == nil {
		t.Fatal("accepted nil context")
	}
}

func referenceFlags(opts Options) uint64 {
	var flags uint64
	if opts.Minimal {
		flags |= 1
	}
	if opts.Algorithm == Patience {
		flags |= 1 << 14
	}
	if opts.Algorithm == Histogram {
		flags |= 1 << 15
	}
	flags |= uint64(opts.Whitespace) << 1
	if opts.IgnoreBlankLines {
		flags |= 1 << 7
	}
	if opts.IndentHeuristic {
		flags |= 1 << 23
	}
	return flags
}

func oracle(t *testing.T) *testref.Oracle {
	t.Helper()
	if os.Getenv("GO_XDIFF_TESTREF") != "1" {
		t.Skip("set GO_XDIFF_TESTREF=1 to compare the pinned C reference")
	}
	return testref.New(t)
}

func compareScript(t *testing.T, ref *testref.Oracle, a, b []byte, opts Options) {
	t.Helper()
	r, err := Diff(context.Background(), a, b, &opts)
	if err != nil {
		t.Fatal(err)
	}
	checkChunks(t, a, b, r)
	expected := ref.ScriptWithAnchors(t, a, b, referenceFlags(opts), opts.Anchors)
	got := r.Changes()
	if len(expected) != len(got) {
		t.Fatalf("script mismatch opts=%+v\nold=%q\nnew=%q\ngot=%+v\nwant=%+v", opts, a, b, got, expected)
	}
	for i, c := range expected {
		want := Change{c.OldStart, c.OldCount, c.NewStart, c.NewCount}
		if got[i] != want {
			t.Fatalf("script mismatch opts=%+v\nold=%q\nnew=%q\ngot=%+v\nwant=%+v", opts, a, b, got, expected)
		}
	}
}

func randomText(rng *rand.Rand, n int) []byte {
	var data []byte
	lines := []string{"a\n", "b\n", "c\n", "\n", "  x\n", "\tx\n", "}\n", "f() {\n", "a\r\n", "\xff\n"}
	for range n {
		data = append(data, lines[rng.Intn(len(lines))]...)
	}
	if len(data) > 0 && rng.Intn(4) == 0 {
		data = data[:len(data)-1]
	}
	return data
}

func TestMyersReference(t *testing.T) {
	ref := oracle(t)
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 250; i++ {
		a, b := randomText(rng, rng.Intn(50)), randomText(rng, rng.Intn(50))
		opts := Options{Minimal: i%3 == 0, IndentHeuristic: i%2 == 0}
		if i%5 != 0 {
			opts.Whitespace = Whitespace(1 << uint(i%4))
		}
		compareScript(t, ref, a, b, opts)
	}
	for i := 0; i < 8; i++ {
		a, b := randomText(rng, 1200), randomText(rng, 1300)
		compareScript(t, ref, a, b, Options{Minimal: i%2 == 0, IndentHeuristic: i%3 == 0})
	}
	for _, mode := range []Whitespace{0, IgnoreAllSpace, IgnoreSpaceChange, IgnoreSpaceAtEOL, IgnoreCRAtEOL} {
		for _, pair := range [][2]string{
			{"a\vb\n", "ab\n"}, {"a\fb\n", "ab\n"}, {"a\n", "a"},
			{"a\r\n", "a\n"}, {"a\r", "a"}, {" a \n", "\ta\t\n"},
			{"a  b\n", "a b\n"}, {"a b\n", "ab\n"},
		} {
			compareScript(t, ref, []byte(pair[0]), []byte(pair[1]), Options{Whitespace: mode})
		}
	}
}

func FuzzDiff(f *testing.F) {
	f.Add([]byte("a\nb\n"), []byte("a\nc\n"), uint8(0))
	f.Add([]byte{0xff, 0, '\n'}, []byte{0, '\n', 0xfe}, uint8(1))
	f.Fuzz(func(t *testing.T, a, b []byte, flags uint8) {
		if len(a)+len(b) > 20000 {
			t.Skip()
		}
		algorithm := Algorithm((flags >> 6) % 3)
		opts := Options{Algorithm: algorithm, Minimal: algorithm == Myers && flags&1 != 0,
			IndentHeuristic: flags&2 != 0, Whitespace: Whitespace((flags >> 2) & 15),
			IgnoreBlankLines: flags&8 != 0}
		r, err := Diff(context.Background(), a, b, &opts)
		if err != nil {
			t.Fatal(err)
		}
		checkChunks(t, a, b, r)
		emit := EmitOptions{Context: int(flags % 4), InterHunkContext: int((flags >> 2) % 4),
			FunctionNames: flags&4 != 0, FunctionContext: flags&8 != 0}
		hunks, err := r.Hunks(context.Background(), &emit)
		if err != nil {
			t.Fatal(err)
		}
		for _, h := range hunks {
			if h.OldStart < 0 || h.NewStart < 0 || h.OldLines < 0 || h.NewLines < 0 ||
				h.OldStart+h.OldLines > len(r.a.recs) || h.NewStart+h.NewLines > len(r.b.recs) {
				t.Fatalf("hunk outside inputs: %+v", h.Change)
			}
		}
		if err := r.WriteUnified(context.Background(), io.Discard, &emit); err != nil {
			t.Fatal(err)
		}
	})
}

func ExampleDiff() {
	result, err := Diff(context.Background(), []byte("one\ntwo\n"), []byte("one\nthree\n"), nil)
	if err != nil {
		panic(err)
	}
	for _, change := range result.Changes() {
		fmt.Printf("old %d,%d -> new %d,%d\n", change.OldStart, change.OldLines, change.NewStart, change.NewLines)
	}
	// Output:
	// old 1,1 -> new 1,1
}
