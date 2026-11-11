package xdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"testing"
	"time"
)

func TestMergeBehavior(t *testing.T) {
	for _, tc := range []struct {
		name, base, ours, theirs, want string
		options                        *MergeOptions
		conflicts                      int
	}{
		{
			name: "independent edits", base: "a\nb\nc\n", ours: "A\nb\nc\n", theirs: "a\nb\nC\n",
			want: "A\nb\nC\n",
		},
		{
			name: "nil refines conflicts", base: "base\n",
			ours: "same\nours\nend\n", theirs: "same\ntheirs\nend\n",
			want: "same\n<<<<<<<\nours\n=======\ntheirs\n>>>>>>>\nend\n", conflicts: 1,
		},
		{
			name: "nil accepts identical edits", base: "base\n", ours: "same\n", theirs: "same\n",
			want: "same\n",
		},
		{
			name: "zero level keeps identical conflict", base: "base\n", ours: "same\n", theirs: "same\n",
			options: &MergeOptions{}, want: "<<<<<<<\nsame\n=======\nsame\n>>>>>>>\n", conflicts: 1,
		},
		{
			name: "unchanged ours", base: "base", ours: "base", theirs: "theirs",
			want: "theirs",
		},
		{
			name: "unchanged theirs", base: "base", ours: "ours", theirs: "base",
			want: "ours",
		},
		{
			name: "no-script side uses other bytes", base: "base\n", ours: "base  \n", theirs: "theirs",
			options: &MergeOptions{Diff: Options{Whitespace: IgnoreSpaceAtEOL}}, want: "theirs",
		},
		{
			name: "both scripts empty use theirs", base: "base\n", ours: "base \n", theirs: "base\t\n",
			options: &MergeOptions{Diff: Options{Whitespace: IgnoreSpaceAtEOL}}, want: "base\t\n",
		},
		{
			name: "blank suppression does not discard scripts", ours: "\n", theirs: "\n\n",
			options: &MergeOptions{Diff: Options{IgnoreBlankLines: true}},
			want:    "<<<<<<<\n\n=======\n\n\n>>>>>>>\n", conflicts: 1,
		},
		{
			name: "regex suppression does not discard scripts", base: "base\n", ours: "ours\n", theirs: "theirs\n",
			options: &MergeOptions{Diff: Options{IgnoreRegex: []*regexp.Regexp{regexp.MustCompile(".*")}}},
			want:    "<<<<<<<\nours\n=======\ntheirs\n>>>>>>>\n", conflicts: 1,
		},
		{
			name: "labels and missing newline", base: "base", ours: "ours", theirs: "theirs",
			options: &MergeOptions{Style: ConflictDiff3, MarkerSize: 3,
				AncestorLabel: "B", OursLabel: "O\nraw\x00unused", TheirsLabel: "T"},
			want: "<<< O\nraw\nours\n||| B\nbase\n===\ntheirs\n>>> T\n", conflicts: 1,
		},
		{
			name: "union inserts newline only between sides", base: "base", ours: "ours", theirs: "theirs",
			options: &MergeOptions{Favor: FavorUnion}, want: "ours\ntheirs",
		},
		{
			name: "union empty ours adds no newline", base: "base", theirs: "theirs",
			options: &MergeOptions{Favor: FavorUnion}, want: "theirs",
		},
		{
			name: "empty merge", want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Merge(context.Background(), []byte(tc.base), []byte(tc.ours), []byte(tc.theirs), tc.options)
			if err != nil {
				t.Fatal(err)
			}
			if string(r.Content) != tc.want || r.Conflicts != tc.conflicts {
				t.Fatalf("got (%d) %q; want (%d) %q", r.Conflicts, r.Content, tc.conflicts, tc.want)
			}
		})
	}
}

func TestMergeConflictSeparation(t *testing.T) {
	for _, tc := range []struct {
		gap       string
		level     MergeLevel
		conflicts int
	}{
		{"one\ntwo\nthree\n", MergeZealous, 1},
		{"one\ntwo\nthree\nfour\n", MergeZealous, 2},
		{"{\n\n}\n\n", MergeZealous, 2},
		{"{\n\n}\n\n", MergeZealousAlnum, 1},
		{"{\n0\n}\n\n", MergeZealousAlnum, 2},
		{"{\nA\n}\n\n", MergeZealousAlnum, 2},
		{"{\na\n}\n\n", MergeZealousAlnum, 2},
		{"{\n_\n}\n\n", MergeZealousAlnum, 1},
		{"{\n\xff\n}\n\n", MergeZealousAlnum, 1},
	} {
		t.Run(fmt.Sprintf("%d/%q", tc.level, tc.gap), func(t *testing.T) {
			ours, theirs := []byte("ours1\n"+tc.gap+"ours2\n"), []byte("theirs1\n"+tc.gap+"theirs2\n")
			r, err := Merge(context.Background(), []byte("base\n"), ours, theirs, &MergeOptions{Level: tc.level})
			if err != nil {
				t.Fatal(err)
			}
			if r.Conflicts != tc.conflicts {
				t.Fatalf("got %d conflicts; want %d: %q", r.Conflicts, tc.conflicts, r.Content)
			}
		})
	}
}

func TestMergeMarkerLineEndings(t *testing.T) {
	for _, tc := range []struct {
		name, base, ours, theirs, want string
		favor                          MergeFavor
	}{
		{
			name: "all CRLF", base: "base\r\n", ours: "ours\r\n", theirs: "theirs\r\n",
			want: "<<<<<<<\r\nours\r\n=======\r\ntheirs\r\n>>>>>>>\r\n",
		},
		{
			name: "ancestor LF forces LF markers", base: "base\n", ours: "ours\r\n", theirs: "theirs\r\n",
			want: "<<<<<<<\nours\r\n=======\ntheirs\r\n>>>>>>>\n",
		},
		{
			name: "ancestor CRLF determines incomplete sides", base: "base\r\n", ours: "ours", theirs: "theirs",
			want: "<<<<<<<\r\nours\r\n=======\r\ntheirs\r\n>>>>>>>\r\n",
		},
		{
			name: "unknown ancestor defaults to LF", base: "base", ours: "ours\r\n", theirs: "theirs\r\n",
			want: "<<<<<<<\nours\r\n=======\ntheirs\r\n>>>>>>>\n",
		},
		{
			name: "mixed sides use LF markers", base: "base\r\n", ours: "ours\r\n", theirs: "theirs\n",
			want: "<<<<<<<\nours\r\n=======\ntheirs\n>>>>>>>\n",
		},
		{
			name: "previous line determines incomplete side", base: "same\r\nbase",
			ours: "same\r\nours", theirs: "same\r\ntheirs",
			want: "same\r\n<<<<<<<\r\nours\r\n=======\r\ntheirs\r\n>>>>>>>\r\n",
		},
		{
			name: "unknown styles use LF", base: "base", ours: "ours", theirs: "theirs",
			want: "<<<<<<<\nours\n=======\ntheirs\n>>>>>>>\n",
		},
		{
			name: "union missing newline follows CRLF", base: "base\r\n", ours: "ours", theirs: "theirs",
			favor: FavorUnion, want: "ours\r\ntheirs",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := Merge(context.Background(), []byte(tc.base), []byte(tc.ours), []byte(tc.theirs),
				&MergeOptions{Favor: tc.favor})
			if err != nil {
				t.Fatal(err)
			}
			if string(r.Content) != tc.want {
				t.Fatalf("got %q; want %q", r.Content, tc.want)
			}
		})
	}
}

func TestMergeOptionsAndInputsUnchanged(t *testing.T) {
	for _, style := range []ConflictStyle{ConflictMerge, ConflictDiff3, ConflictZDiff3} {
		for _, favor := range []MergeFavor{FavorConflict, FavorOurs, FavorTheirs, FavorUnion} {
			opts := MergeOptions{Level: MergeZealousAlnum, Style: style, Favor: favor,
				Diff: Options{Algorithm: Patience, Anchors: []string{"same"},
					IgnoreRegex: []*regexp.Regexp{regexp.MustCompile("never")}}}
			before := opts
			before.Diff.Anchors = append([]string(nil), opts.Diff.Anchors...)
			before.Diff.IgnoreRegex = append([]*regexp.Regexp(nil), opts.Diff.IgnoreRegex...)
			base, ours, theirs := []byte("base\n"), []byte("same\nours\n"), []byte("same\ntheirs\n")
			r, err := Merge(context.Background(), base, ours, theirs, &opts)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(opts, before) {
				t.Fatalf("options changed: got %+v; want %+v", opts, before)
			}
			r.Content[0] = '!'
			if string(base) != "base\n" || string(ours) != "same\nours\n" || string(theirs) != "same\ntheirs\n" {
				t.Fatal("merge changed input bytes or result aliases inputs")
			}
		}
	}
	for _, oursUnchanged := range []bool{false, true} {
		base, ours, theirs := []byte("base\n"), []byte("ours\n"), []byte("theirs\n")
		if oursUnchanged {
			ours = bytes.Clone(base)
		} else {
			theirs = bytes.Clone(base)
		}
		originalOurs, originalTheirs := bytes.Clone(ours), bytes.Clone(theirs)
		r, err := Merge(context.Background(), base, ours, theirs, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Content[0] = '!'
		if !bytes.Equal(ours, originalOurs) || !bytes.Equal(theirs, originalTheirs) {
			t.Fatal("fast-path result aliases an input")
		}
	}
}

func TestMergeUnchangedAllocations(t *testing.T) {
	base := bytes.Repeat([]byte("base\n"), 128)
	changed := bytes.Repeat([]byte("changed\n"), 128)
	for _, algorithm := range []Algorithm{Myers, Patience} {
		for _, pair := range []struct {
			name               string
			ours, theirs, want []byte
		}{
			{"both", bytes.Clone(base), bytes.Clone(base), base},
			{"ours", bytes.Clone(base), changed, changed},
			{"theirs", changed, bytes.Clone(base), changed},
		} {
			t.Run(fmt.Sprintf("%d/%s", algorithm, pair.name), func(t *testing.T) {
				ctx := t.Context()
				opts := MergeOptions{Diff: Options{Algorithm: algorithm}}
				var result *MergeResult
				allocs := testing.AllocsPerRun(10, func() {
					var err error
					result, err = Merge(ctx, base, pair.ours, pair.theirs, &opts)
					if err != nil {
						t.Fatal(err)
					}
				})
				if allocs > 2 {
					t.Fatalf("unchanged-side merge: %.0f allocations, want at most 2", allocs)
				}
				if result.Conflicts != 0 || !bytes.Equal(result.Content, pair.want) {
					t.Fatalf("unchanged-side merge: %+v", result)
				}
			})
		}
	}
}

func TestMergeWhitespaceAllocationScaling(t *testing.T) {
	for _, mode := range []Whitespace{IgnoreAllSpace, IgnoreSpaceChange} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			ctx := t.Context()
			opts := MergeOptions{Level: MergeEager, Diff: Options{Whitespace: mode}}
			measure := func(lines int) float64 {
				base := bytes.Repeat([]byte("original\n"), lines)
				ours := bytes.Repeat([]byte("one two\n"), lines)
				theirs := bytes.Repeat([]byte("one  two\n"), lines)
				var result *MergeResult
				allocs := testing.AllocsPerRun(10, func() {
					var err error
					result, err = Merge(ctx, base, ours, theirs, &opts)
					if err != nil {
						t.Fatal(err)
					}
				})
				if result.Conflicts != 0 || !bytes.Equal(result.Content, ours) {
					t.Fatalf("normalized merge: %+v", result)
				}
				return allocs
			}
			small, large := measure(128), measure(1024)
			if large > small+2 {
				t.Fatalf("allocations scale with line count: %.0f -> %.0f", small, large)
			}
		})
	}
}

func TestMergeHistogramUnchangedSide(t *testing.T) {
	base, changed := histogramCollisionText(histogramMaxChain + 1)
	for _, pair := range [][2][]byte{{bytes.Clone(base), changed}, {changed, bytes.Clone(base)}} {
		result, err := Merge(t.Context(), base, pair[0], pair[1],
			&MergeOptions{Diff: Options{Algorithm: Histogram}})
		want := fmt.Sprintf("xdiff: histogram hash chain exceeds %d records", histogramMaxChain)
		if err == nil || err.Error() != want || result != nil {
			t.Fatalf("histogram merge: result=%+v, error=%v; want %q", result, err, want)
		}
	}
}

func TestMergeErrors(t *testing.T) {
	for _, opts := range []MergeOptions{
		{Level: 99}, {Favor: 99}, {Style: 99}, {MarkerSize: -1},
		{Diff: Options{Algorithm: 99}},
		{Diff: Options{Whitespace: 128}},
		{Diff: Options{Algorithm: Histogram, Minimal: true}},
		{Diff: Options{IgnoreRegex: []*regexp.Regexp{nil}}},
	} {
		if r, err := Merge(context.Background(), nil, nil, nil, &opts); err == nil || r != nil {
			t.Fatalf("accepted invalid options: %+v, result=%+v, error=%v", opts, r, err)
		}
	}
	if r, err := Merge(nil, nil, nil, nil, nil); err == nil || r != nil {
		t.Fatalf("nil context: result=%+v, error=%v", r, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r, err := Merge(ctx, nil, nil, nil, nil); !errors.Is(err, context.Canceled) || r != nil {
		t.Fatalf("canceled context: result=%+v, error=%v", r, err)
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Time{})
	defer cancel()
	if _, err := Merge(ctx, nil, nil, nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired context: %v", err)
	}
}

func TestMergeLoopCancellation(t *testing.T) {
	base := bytes.Repeat([]byte("base\n"), 100)
	ours := bytes.Repeat([]byte("ours\n"), 100)
	theirs := bytes.Repeat([]byte("theirs\n"), 100)
	for _, remaining := range []int{2, 100, 300, 600} {
		ctx := &algorithmCancelContext{Context: context.Background(), remaining: remaining}
		r, err := Merge(ctx, base, ours, theirs, nil)
		if !errors.Is(err, context.Canceled) || r != nil {
			t.Fatalf("cancellation at %d: result=%+v, error=%v", remaining, r, err)
		}
	}
	ctx := &algorithmCancelContext{Context: context.Background(), remaining: 2}
	o := mergeOutput{ctx: ctx}
	if err := o.copy(bytes.Repeat([]byte{'x'}, 3*65536)); !errors.Is(err, context.Canceled) {
		t.Fatalf("copy cancellation: %v", err)
	}
}

func TestMergeRefineIdenticalRegion(t *testing.T) {
	r, err := Diff(t.Context(), []byte("base\n"), []byte("same\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	m := merger{ctx: t.Context(), ours: r, theirs: r}
	c := &mergeChange{chg0: 1, chg1: 1, chg2: 1}
	if err := m.refine(c); err != nil {
		t.Fatal(err)
	}
	output := mergeOutput{ctx: t.Context()}
	result, err := m.output(c, &output)
	if err != nil {
		t.Fatal(err)
	}
	if result.Conflicts != 0 || string(result.Content) != "same\n" {
		t.Fatalf("refined identical region: %+v", result)
	}
}

func TestMergeIncompleteLineEndings(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
	}{
		{"first\r\nlast", 1},
		{"first\nlast", 0},
		{"last", -1},
	} {
		var classes classifier
		f, err := readFile(t.Context(), []byte(tc.text), 0, &classes)
		if err != nil {
			t.Fatal(err)
		}
		if got := mergeEOLCRLF(&f, len(f.recs)-1); got != tc.want {
			t.Fatalf("line ending for %q = %d, want %d", tc.text, got, tc.want)
		}
	}
}

func ExampleMerge() {
	result, err := Merge(context.Background(), []byte("base\n"),
		[]byte("same\nours\nend\n"), []byte("same\ntheirs\nend\n"), nil)
	if err != nil {
		panic(err)
	}
	fmt.Print(string(result.Content))
	fmt.Println("conflicts:", result.Conflicts)
	// Output:
	// same
	// <<<<<<<
	// ours
	// =======
	// theirs
	// >>>>>>>
	// end
	// conflicts: 1
}

func ExampleMerge_styles() {
	for _, style := range []ConflictStyle{ConflictMerge, ConflictDiff3, ConflictZDiff3} {
		options := &MergeOptions{Level: MergeZealous, Style: style, MarkerSize: 3,
			AncestorLabel: "base", OursLabel: "left", TheirsLabel: "right"}
		result, err := Merge(context.Background(), []byte("base\n"),
			[]byte("same\nours\nend\n"), []byte("same\ntheirs\nend\n"), options)
		if err != nil {
			panic(err)
		}
		fmt.Printf("style %d:\n%s", style, result.Content)
	}
	// Output:
	// style 0:
	// same
	// <<< left
	// ours
	// ===
	// theirs
	// >>> right
	// end
	// style 1:
	// <<< left
	// same
	// ours
	// end
	// ||| base
	// base
	// ===
	// same
	// theirs
	// end
	// >>> right
	// style 2:
	// same
	// <<< left
	// ours
	// ||| base
	// base
	// ===
	// theirs
	// >>> right
	// end
}

func ExampleMerge_favors() {
	for _, favor := range []MergeFavor{FavorOurs, FavorTheirs, FavorUnion} {
		result, err := Merge(context.Background(), []byte("base"), []byte("ours"), []byte("theirs"),
			&MergeOptions{Favor: favor})
		if err != nil {
			panic(err)
		}
		fmt.Printf("favor %d: %q, conflicts: %d\n", favor, result.Content, result.Conflicts)
	}
	// Output:
	// favor 1: "ours", conflicts: 0
	// favor 2: "theirs", conflicts: 0
	// favor 3: "ours\ntheirs", conflicts: 0
}

func ExampleMerge_levels() {
	for _, level := range []MergeLevel{MergeMinimal, MergeEager, MergeZealous, MergeZealousAlnum} {
		result, err := Merge(context.Background(), []byte("base\n"), []byte("same\n"), []byte("same\n"),
			&MergeOptions{Level: level})
		if err != nil {
			panic(err)
		}
		fmt.Printf("level %d, conflicts: %d\n", level, result.Conflicts)
	}
	// Output:
	// level 0, conflicts: 1
	// level 1, conflicts: 0
	// level 2, conflicts: 0
	// level 3, conflicts: 0
}

func ExampleMerge_crlfMarkers() {
	result, err := Merge(context.Background(), []byte("base\r\n"), []byte("ours"), []byte("theirs"), nil)
	if err != nil {
		panic(err)
	}
	fmt.Printf("%q\n", result.Content)
	// Output:
	// "<<<<<<<\r\nours\r\n=======\r\ntheirs\r\n>>>>>>>\r\n"
}
