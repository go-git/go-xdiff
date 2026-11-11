package xdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/go-git/go-xdiff/internal/testref"
)

func compareOutput(t *testing.T, ref *testref.Oracle, a, b []byte, opts Options, emit EmitOptions, patterns []string) {
	t.Helper()
	r, err := Diff(context.Background(), a, b, &opts)
	if err != nil {
		t.Fatal(err)
	}
	var flags uint64
	if emit.FunctionNames {
		flags |= 1
	}
	if emit.NoHunkHeader {
		flags |= 2
	}
	if emit.FunctionContext {
		flags |= 4
	}
	want := ref.Diff(t, a, b, testref.DiffOptions{
		Flags: referenceFlags(opts), Context: emit.Context, Interhunk: emit.InterHunkContext,
		EmitFlags: flags, Anchors: opts.Anchors, IgnorePatterns: patterns,
	})
	var got bytes.Buffer
	if err := WriteUnified(context.Background(), &got, r, &emit); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Fatalf("output mismatch\nold %q\nnew %q\noptions %+v\nemit %+v\ngot %q\nwant %q",
			a, b, opts, emit, got.Bytes(), want)
	}
}

func TestOutputReference(t *testing.T) {
	ref := oracle(t)
	pairs := [][2]string{
		{"", "a"}, {"a", ""}, {"a", "b"}, {"a\n", "a"}, {"a", "a\n"},
		{"a\nb\nc\n", "a\nB\nc\n"},
		{"function\n old\n}\n\nnext\n old\n}\n", "function\n new\n}\n\nnext\n new\n}\n"},
		{"header\n line\n\n line\n", "header\n line\n\n line\n tail\n"},
		{"header\n line\n", "header\n line\nnewfunction\n body\n"},
		{"\n\nx\n\n\nz\n", "\nx\n\nz\n"},
		{"x\n \ny\n", "x\n\t\ny\n"},
		{"a\nb\n", " a \nB\n"},
		{"x\n1\n2\n3\n4\n\nx\n\n", "X\n1\n2\n3\n4\nx\n"},
		{"\nfirst\n body\n}\n\nsecond\n old\n}\n", "first\n body\n}\n\nsecond\n new\n}\n"},
		{"éheader\n body\n", "éheader\n new\n"},
		{"$name \v\f\n body\n", "$name \v\f\n new\n"},
		{strings.Repeat("X", 85) + " \n old\n", strings.Repeat("X", 85) + " \n new\n"},
	}
	for _, pair := range pairs {
		for flags := 0; flags < 8; flags++ {
			for _, contextLines := range []int{0, 1, 3} {
				emit := EmitOptions{Context: contextLines, InterHunkContext: 1,
					FunctionNames: flags&1 != 0, NoHunkHeader: flags&2 != 0, FunctionContext: flags&4 != 0}
				for _, opts := range []Options{{}, {IgnoreBlankLines: true}, {Whitespace: IgnoreAllSpace, IgnoreBlankLines: true}} {
					compareOutput(t, ref, []byte(pair[0]), []byte(pair[1]), opts, emit, nil)
				}
			}
		}
	}
}

func TestOutputRandomReference(t *testing.T) {
	ref := oracle(t)
	rng := rand.New(rand.NewSource(9102026))
	for i := 0; i < 400; i++ {
		a := randomText(rng, rng.Intn(60))
		b := randomText(rng, rng.Intn(60))
		opts := Options{Algorithm: Algorithm(i % 3), IgnoreBlankLines: i%2 == 0, IndentHeuristic: i%3 == 0}
		if i%5 != 0 {
			opts.Whitespace = Whitespace(1 << uint(i%4))
		}
		if opts.Algorithm == Patience {
			opts.Anchors = []string{"a", "f()"}
		}
		emit := EmitOptions{Context: rng.Intn(5), InterHunkContext: rng.Intn(5),
			FunctionNames: i&1 != 0, NoHunkHeader: i&2 != 0, FunctionContext: i&4 != 0}
		compareOutput(t, ref, a, b, opts, emit, nil)
	}
}

func TestOutputIgnoredReference(t *testing.T) {
	ref := oracle(t)
	rng := rand.New(rand.NewSource(47))
	lines := []string{"\n", " \n", "\t\n", "IGNORE old\n", "IGNORE new\n", "keep\n", "FUNCTION\n", " body\n", "}\n"}
	for i := 0; i < 600; i++ {
		var a, b []byte
		for n := rng.Intn(50); n > 0; n-- {
			line := lines[rng.Intn(len(lines))]
			a = append(a, line...)
			switch rng.Intn(5) {
			case 0:
			case 1:
				b = append(b, lines[rng.Intn(len(lines))]...)
			default:
				b = append(b, line...)
			}
			if rng.Intn(5) == 0 {
				b = append(b, lines[rng.Intn(len(lines))]...)
			}
		}
		patterns := []string{"^IGNORE", "^$"}
		opts := Options{IgnoreBlankLines: i%2 == 0, IgnoreRegex: []*regexp.Regexp{
			regexp.MustCompile("(?m)^IGNORE"), regexp.MustCompile("(?m)^$"),
		}}
		if i%3 == 0 {
			opts.Whitespace = IgnoreAllSpace
		}
		emit := EmitOptions{Context: rng.Intn(4), InterHunkContext: rng.Intn(5),
			FunctionNames: i%2 == 0, FunctionContext: i%3 == 0}
		compareOutput(t, ref, a, b, opts, emit, patterns)
	}
	for _, pattern := range []string{"^$", "old$", "old\n$", "\r$", "\n", "IGNORE", "x.y"} {
		opts := Options{IgnoreRegex: []*regexp.Regexp{regexp.MustCompile("(?m)" + pattern)}}
		for _, pair := range [][2]string{
			{"old\n", ""}, {"old", ""}, {"\n", ""}, {"old\r\n", ""},
			{"IGNORE\x00old\n", ""}, {"x\ny\n", ""}, {"x\vy\n", ""},
		} {
			compareOutput(t, ref, []byte(pair[0]), []byte(pair[1]), opts, EmitOptions{}, []string{pattern})
		}
	}
}

func TestHunks(t *testing.T) {
	a, b := []byte("name\n old\n same\n"), []byte("name \n new\n same \n")
	r, err := Diff(context.Background(), a, b, &Options{Whitespace: IgnoreAllSpace})
	if err != nil {
		t.Fatal(err)
	}
	opts := EmitOptions{Context: 1, FunctionNames: true}
	hunks, err := r.Hunks(context.Background(), &opts)
	if err != nil {
		t.Fatal(err)
	}
	want := []Hunk{{Change: Change{0, 3, 0, 3}, Lines: []Line{
		{Equal, []byte("name \n")}, {Delete, []byte(" old\n")},
		{Insert, []byte(" new\n")}, {Equal, []byte(" same \n")},
	}}}
	if !reflect.DeepEqual(hunks, want) {
		t.Fatalf("hunks: %#v", hunks)
	}
	if &hunks[0].Lines[0].Content[0] != &b[0] || &hunks[0].Lines[1].Content[0] != &a[5] {
		t.Fatal("hunk lines do not refer to inputs")
	}
	opts.NoHunkHeader = true
	without, err := r.Hunks(context.Background(), &opts)
	if err != nil || !reflect.DeepEqual(hunks, without) {
		t.Fatalf("NoHunkHeader changes hunks: %v", err)
	}
}

func TestOutputFiltersDoNotChangeScript(t *testing.T) {
	opts := Options{IgnoreBlankLines: true, IgnoreRegex: []*regexp.Regexp{regexp.MustCompile("IGNORE")}}
	r, err := Diff(context.Background(), []byte("IGNORE old\n\n"), []byte("IGNORE new\n\n"), &opts)
	if err != nil {
		t.Fatal(err)
	}
	changes, chunks := r.Changes(), r.Chunks()
	for i := 0; i < 2; i++ {
		hunks, err := r.Hunks(context.Background(), &EmitOptions{FunctionContext: true})
		if err != nil || len(hunks) != 0 {
			t.Fatalf("suppressed hunks: %v, %v", hunks, err)
		}
		if err := WriteUnified(context.Background(), io.Discard, r, nil); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(changes, r.Changes()) || !reflect.DeepEqual(chunks, r.Chunks()) {
		t.Fatal("output filters changed the edit script")
	}
	if !opts.IgnoreBlankLines || opts.IgnoreRegex[0].String() != "IGNORE" {
		t.Fatal("output changed caller options")
	}
}

func TestFunctionNames(t *testing.T) {
	for _, tc := range []struct {
		header string
		find   func([]byte) (string, bool)
		want   string
	}{
		{strings.Repeat("X", 79) + " tail\n", nil, strings.Repeat("X", 79)},
		{"_name \t\r\n", nil, "_name"},
		{"$name\v\f\n", nil, "$name\v\f"},
		{"éname\n", nil, ""},
		{"@name\n", func(line []byte) (string, bool) {
			return strings.Repeat("N", 81), bytes.HasPrefix(line, []byte("@"))
		}, strings.Repeat("N", 80)},
		{"@name\n", func(line []byte) (string, bool) {
			return "custom \t", bytes.HasPrefix(line, []byte("@"))
		}, "custom \t"},
	} {
		a, n := []byte(tc.header+" old\n}\n"), []byte(tc.header+" new\n}\n")
		r, err := Diff(context.Background(), a, n, nil)
		if err != nil {
			t.Fatal(err)
		}
		emit := EmitOptions{FunctionNames: true, FindFunc: tc.find}
		hunks, err := r.Hunks(context.Background(), &emit)
		if err != nil {
			t.Fatal(err)
		}
		if len(hunks) != 1 || hunks[0].Function != tc.want {
			t.Fatalf("function name: %#v, want %q", hunks, tc.want)
		}
		if !emit.FunctionNames || emit.Context != 0 || emit.FunctionContext {
			t.Fatal("hunk construction changed emission options")
		}
	}
}

func TestOutputRegexUsesRawRecords(t *testing.T) {
	for _, tc := range []struct {
		pattern, record string
		suppressed      bool
	}{
		{"^old$", "old\n", false},
		{"(?m)^old$", "old\n", true},
		{"^old$", "old", true},
		{"(?m)^old$", "old\r\n", false},
		{"\n$", "old\n", true},
		{"\x00", "old\x00\n", true},
	} {
		r, err := Diff(context.Background(), []byte(tc.record), nil,
			&Options{IgnoreRegex: []*regexp.Regexp{regexp.MustCompile(tc.pattern)}})
		if err != nil {
			t.Fatal(err)
		}
		hunks, err := r.Hunks(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if (len(hunks) == 0) != tc.suppressed {
			t.Fatalf("pattern %q, record %q: hunks %v", tc.pattern, tc.record, hunks)
		}
	}
}

func TestOutputHugeContext(t *testing.T) {
	ref := oracle(t)
	maxInt := int(^uint(0) >> 1)
	for _, emit := range []EmitOptions{
		{Context: maxInt}, {InterHunkContext: maxInt},
		{Context: maxInt, InterHunkContext: maxInt, FunctionContext: true},
	} {
		compareOutput(t, ref, []byte("a\nb\nc\nd\ne\n"), []byte("A\nb\nc\nd\nE\n"), Options{}, emit, nil)
	}
}

type outputWriter struct {
	err    error
	short  bool
	cancel context.CancelFunc
}

func (w outputWriter) Write(p []byte) (int, error) {
	if w.cancel != nil {
		w.cancel()
	}
	if w.short {
		return len(p) - 1, w.err
	}
	return len(p), w.err
}

func TestOutputErrors(t *testing.T) {
	r, err := Diff(context.Background(), []byte("a\n"), []byte("b\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	callbacks := EmitCallbacks{LineFunc: func(...[]byte) error { return nil }}
	for _, emit := range []*EmitOptions{{Context: -1}, {InterHunkContext: -1}} {
		if _, err := r.Hunks(context.Background(), emit); err == nil {
			t.Fatal("accepted negative context")
		}
		if err := WriteUnified(context.Background(), io.Discard, r, emit); err == nil {
			t.Fatal("writer accepted negative context")
		}
		if err := r.Emit(t.Context(), emit, &callbacks); err == nil {
			t.Fatal("emitter accepted negative context")
		}
	}
	if _, err := r.Hunks(nil, nil); err == nil {
		t.Fatal("accepted nil context")
	}
	if err := WriteUnified(nil, io.Discard, r, nil); err == nil {
		t.Fatal("writer accepted nil context")
	}
	if err := r.Emit(nil, nil, &callbacks); err == nil {
		t.Fatal("emitter accepted nil context")
	}
	if err := WriteUnified(context.Background(), nil, r, nil); err == nil {
		t.Fatal("accepted nil writer")
	}
	failure := errors.New("writer failure")
	for _, tc := range []struct {
		writer outputWriter
		want   error
	}{{outputWriter{err: failure}, failure}, {outputWriter{short: true}, io.ErrShortWrite},
		{outputWriter{short: true, err: failure}, failure}} {
		if err := WriteUnified(context.Background(), tc.writer, r, nil); !errors.Is(err, tc.want) {
			t.Fatalf("write error: %v, want %v", err, tc.want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := WriteUnified(ctx, outputWriter{cancel: cancel}, r, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("writer cancellation: %v", err)
	}
	if _, err := r.Hunks(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("hunk cancellation: %v", err)
	}
}

func TestEmitAllocations(t *testing.T) {
	old, new, _ := benchmarkInputs()
	for _, spec := range []struct {
		name     string
		new      []byte
		noHeader bool
		header   func(Hunk) error
		overhead int
	}{
		{"default", new, false, nil, 4},
		{"no header", new, true, nil, 3},
		{"custom header", new, false, func(Hunk) error { return nil }, 3},
		{"unchanged", bytes.Clone(old), false, nil, 2},
	} {
		t.Run(spec.name, func(t *testing.T) {
			ctx := t.Context()
			result, err := Diff(ctx, old, spec.new, nil)
			if err != nil {
				t.Fatal(err)
			}
			opts := EmitOptions{Context: 3, NoHunkHeader: spec.noHeader}
			hunks, err := result.Hunks(ctx, &opts)
			if err != nil {
				t.Fatal(err)
			}
			callbacks := EmitCallbacks{
				HunkHeaderFunc: spec.header,
				LineFunc:       func(...[]byte) error { return nil },
			}
			allocs := testing.AllocsPerRun(10, func() {
				if err := result.Emit(ctx, &opts, &callbacks); err != nil {
					t.Fatal(err)
				}
			})
			if limit := len(hunks) + spec.overhead; allocs > float64(limit) {
				t.Fatalf("emission: %.0f allocations, want at most %d", allocs, limit)
			}
		})
	}
}

func TestEmitRetainedHunks(t *testing.T) {
	result, err := Diff(t.Context(), []byte("old-one\nsame\nold-two\n"),
		[]byte("new-one\nsame\nnew-two\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	var hunks []Hunk
	err = result.Emit(t.Context(), nil, &EmitCallbacks{
		HunkHeaderFunc: func(h Hunk) error {
			hunks = append(hunks, h)
			return nil
		},
		LineFunc: func(...[]byte) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 2 || len(hunks[0].Lines) != 2 || len(hunks[1].Lines) != 2 {
		t.Fatalf("retained hunks: %+v", hunks)
	}
	for i, want := range [][2]string{{"old-one\n", "new-one\n"}, {"old-two\n", "new-two\n"}} {
		if string(hunks[i].Lines[0].Content) != want[0] || string(hunks[i].Lines[1].Content) != want[1] {
			t.Fatalf("retained hunk %d changed: %+v", i, hunks[i])
		}
	}
}

func TestEmitCallbacks(t *testing.T) {
	old, new := []byte("head\nold"), []byte("head\nnew")
	r, err := Diff(t.Context(), old, new, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, customHeader := range []bool{false, true} {
		for _, noHeader := range []bool{false, true} {
			t.Run(fmt.Sprintf("custom=%v/noHeader=%v", customHeader, noHeader), func(t *testing.T) {
				opts := EmitOptions{Context: 1, NoHunkHeader: noHeader}
				before := opts
				var got [][]string
				headerCalls := 0
				callbacks := EmitCallbacks{
					LineFunc: func(parts ...[]byte) error {
						record := make([]string, len(parts))
						for i, part := range parts {
							record[i] = string(part)
						}
						got = append(got, record)
						if len(parts) > 1 {
							content := new
							if parts[0][0] == '-' {
								content = old
							}
							offset := 5
							if parts[0][0] == ' ' {
								offset = 0
							}
							if &parts[1][0] != &content[offset] {
								t.Fatal("record content does not reference the input")
							}
						}
						return nil
					},
				}
				if customHeader {
					callbacks.HunkHeaderFunc = func(h Hunk) error {
						headerCalls++
						if h.Change != (Change{0, 2, 0, 2}) || len(h.Lines) != 3 {
							t.Fatalf("header received unexpected hunk: %+v", h)
						}
						return nil
					}
				}
				if err := r.Emit(t.Context(), &opts, &callbacks); err != nil {
					t.Fatal(err)
				}
				want := [][]string{
					{" ", "head\n"},
					{"-", "old", "\n\\ No newline at end of file\n"},
					{"+", "new", "\n\\ No newline at end of file\n"},
				}
				if !customHeader && !noHeader {
					want = append([][]string{{"@@ -1,2 +1,2 @@\n"}}, want...)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("parts = %q, want %q", got, want)
				}
				wantHeaders := 0
				if customHeader && !noHeader {
					wantHeaders = 1
				}
				if headerCalls != wantHeaders {
					t.Fatalf("header calls = %d, want %d", headerCalls, wantHeaders)
				}
				if !reflect.DeepEqual(opts, before) || string(old) != "head\nold" || string(new) != "head\nnew" {
					t.Fatal("emission modified options or inputs")
				}
			})
		}
	}
}

func TestEmitInvalidCallbacks(t *testing.T) {
	for _, new := range []string{"old\n", "new\n"} {
		r, err := Diff(t.Context(), []byte("old\n"), []byte(new), nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, callbacks := range []*EmitCallbacks{nil, {}, {
			HunkHeaderFunc: func(Hunk) error {
				t.Fatal("header called without a line callback")
				return nil
			},
		}} {
			if err := r.Emit(t.Context(), nil, callbacks); err == nil {
				t.Fatal("accepted a missing line callback")
			}
		}
	}
}

func TestEmitCallbackStops(t *testing.T) {
	r, err := Diff(t.Context(), []byte("head\nold"), []byte("head\nnew"), nil)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("callback failure")
	for _, customHeader := range []bool{false, true} {
		for _, cancelCallback := range []bool{false, true} {
			for stopAt := 1; stopAt <= 4; stopAt++ {
				t.Run(fmt.Sprintf("custom=%v/cancel=%v/at=%d", customHeader, cancelCallback, stopAt), func(t *testing.T) {
					ctx, cancel := context.WithCancel(t.Context())
					defer cancel()
					calls := 0
					consume := func() error {
						calls++
						if calls == stopAt {
							if cancelCallback {
								cancel()
								return nil
							}
							return failure
						}
						return nil
					}
					callbacks := EmitCallbacks{LineFunc: func(...[]byte) error { return consume() }}
					if customHeader {
						callbacks.HunkHeaderFunc = func(Hunk) error { return consume() }
					}
					err := r.Emit(ctx, &EmitOptions{Context: 1}, &callbacks)
					want := failure
					if cancelCallback {
						want = context.Canceled
					}
					if !errors.Is(err, want) || calls != stopAt {
						t.Fatalf("error = %v, calls = %d; want %v at %d", err, calls, want, stopAt)
					}
				})
			}
		}
	}
}

func TestOutputLoopCancellation(t *testing.T) {
	r, err := Diff(context.Background(), bytes.Repeat([]byte("function\n old\n}\n"), 100),
		bytes.Repeat([]byte("function\n new\n}\n"), 100), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, remaining := range []int{2, 100, 300, 500, 800} {
		ctx := &algorithmCancelContext{Context: context.Background(), remaining: remaining}
		if _, err := r.Hunks(ctx, &EmitOptions{FunctionNames: true, FunctionContext: true}); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation at %d: %v", remaining, err)
		}
	}
}

func TestOutputIgnoredChangeGroups(t *testing.T) {
	for _, tc := range []struct {
		name, old, new string
		emit           EmitOptions
		want           Change
	}{
		{"nearby ignored groups", "x\n1\n2\n3\n4\n\nx\n\n", "X\n1\n2\n3\n4\nx\n",
			EmitOptions{Context: 3}, Change{0, 4, 0, 4}},
		{"function after ignored group", "\nfirst\n body\n}\n\nsecond\n old\n}\n",
			"first\n body\n}\n\nsecond\n new\n}\n", EmitOptions{FunctionContext: true}, Change{5, 3, 4, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Diff(t.Context(), []byte(tc.old), []byte(tc.new), &Options{IgnoreBlankLines: true})
			if err != nil {
				t.Fatal(err)
			}
			hunks, err := r.Hunks(t.Context(), &tc.emit)
			if err != nil {
				t.Fatal(err)
			}
			if len(hunks) != 1 || hunks[0].Change != tc.want {
				t.Fatalf("hunks = %+v, want one hunk %+v", hunks, tc.want)
			}
		})
	}
}

func ExampleWriteUnified() {
	result, err := Diff(context.Background(), []byte("one\nold\n"), []byte("one\nnew\n"), nil)
	if err != nil {
		panic(err)
	}
	var output bytes.Buffer
	if err := WriteUnified(context.Background(), &output, result, &EmitOptions{Context: 1}); err != nil {
		panic(err)
	}
	fmt.Print(output.String())
	// Output:
	// @@ -1,2 +1,2 @@
	//  one
	// -old
	// +new
}

func ExampleResult_Hunks() {
	result, err := Diff(context.Background(), []byte("a\nb\nc\nd\ne\n"), []byte("A\nb\nc\nd\nE\n"), nil)
	if err != nil {
		panic(err)
	}
	hunks, err := result.Hunks(context.Background(), &EmitOptions{Context: 1, InterHunkContext: 1})
	if err != nil {
		panic(err)
	}
	for _, hunk := range hunks {
		fmt.Println(hunk.Change)
	}
	// Output: {0 5 0 5}
}

func ExampleWriteUnified_functionNames() {
	result, err := Diff(context.Background(), []byte("function\n old\n}\n"), []byte("function\n new\n}\n"), nil)
	if err != nil {
		panic(err)
	}
	var output bytes.Buffer
	if err := WriteUnified(context.Background(), &output, result, &EmitOptions{FunctionNames: true}); err != nil {
		panic(err)
	}
	fmt.Print(output.String())
	// Output:
	// @@ -2 +2 @@ function
	// - old
	// + new
}

func ExampleWriteUnified_functionContext() {
	result, err := Diff(context.Background(), []byte("@name\n old\n}\n"), []byte("@name\n new\n}\n"), nil)
	if err != nil {
		panic(err)
	}
	emit := EmitOptions{FunctionContext: true, NoHunkHeader: true,
		FindFunc: func(line []byte) (string, bool) {
			return strings.TrimSpace(string(line)), len(line) > 0 && line[0] == '@'
		}}
	var output bytes.Buffer
	if err := WriteUnified(context.Background(), &output, result, &emit); err != nil {
		panic(err)
	}
	fmt.Print(output.String())
	// Output:
	//  @name
	// - old
	// + new
	//  }
}

func ExampleDiff_outputFilters() {
	opts := Options{Whitespace: IgnoreAllSpace, IgnoreBlankLines: true,
		IgnoreRegex: []*regexp.Regexp{regexp.MustCompile("^timestamp:")}}
	result, err := Diff(context.Background(), []byte("value\n\nkeep\ntimestamp: old\n"),
		[]byte(" value \nkeep\ntimestamp: new\n"), &opts)
	if err != nil {
		panic(err)
	}
	hunks, err := result.Hunks(context.Background(), nil)
	if err != nil {
		panic(err)
	}
	fmt.Println("Changes:", len(result.Changes()))
	fmt.Println("Hunks:", len(hunks))
	// Output:
	// Changes: 2
	// Hunks: 0
}

func ExampleResult_Hunks_findFunction() {
	result, err := Diff(context.Background(), []byte("@name\n old\n}\n"), []byte("@name\n new\n}\n"), nil)
	if err != nil {
		panic(err)
	}
	hunks, err := result.Hunks(context.Background(), &EmitOptions{
		FunctionNames: true,
		FindFunc: func(line []byte) (string, bool) {
			return "custom name", bytes.HasPrefix(line, []byte("@"))
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println(hunks[0].Function)
	// Output: custom name
}
