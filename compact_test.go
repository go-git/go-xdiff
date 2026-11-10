package xdiff

import (
	"context"
	"strings"
	"testing"
)

func TestRecordIndent(t *testing.T) {
	for _, tc := range []struct {
		line string
		want int
	}{
		{"", -1}, {" \t\r\n", -1}, {"\tx", 8}, {" \tx", 8},
		{"\r x", 1}, {"\v x", 0}, {"\f x", 0},
		{strings.Repeat(" ", 201), 200},
	} {
		if got := recordIndent([]byte(tc.line)); got != tc.want {
			t.Errorf("indent(%q) = %d, want %d", tc.line, got, tc.want)
		}
	}
}

func TestCompactShiftAndMerge(t *testing.T) {
	e := environment{
		a:   myersTestFile([]int{1, 1, 1}),
		b:   myersTestFile([]int{2, 1, 3}),
		ctx: context.Background(),
	}
	for _, f := range []*file{&e.a, &e.b} {
		f.setChanged(0, true)
		f.setChanged(2, true)
	}
	if err := e.compact(); err != nil {
		t.Fatal(err)
	}
	want := []Change{
		{OldStart: 0, NewStart: 0, NewLines: 1},
		{OldStart: 1, OldLines: 2, NewStart: 2, NewLines: 1},
	}
	got := e.script()
	if len(got) != len(want) {
		t.Fatalf("script = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("script = %#v, want %#v", got, want)
		}
	}
}

func TestCompactGroupSyncError(t *testing.T) {
	for _, tc := range []struct {
		name        string
		a, b        []int
		changeFirst bool
		want        string
	}{
		{"missing other group", []int{1}, nil, false, "xdiff: group sync broken moving to next group"},
		{"extra other group", nil, []int{1}, false, "xdiff: group sync broken at end of file"},
		{"slide past other file", []int{1, 1}, nil, true, "xdiff: group sync broken sliding down"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := environment{a: myersTestFile(tc.a), b: myersTestFile(tc.b), ctx: t.Context()}
			if tc.changeFirst {
				e.a.setChanged(0, true)
			}
			if err := e.compact(); err == nil || err.Error() != tc.want {
				t.Fatalf("invalid group state: %v, want %q", err, tc.want)
			}
		})
	}
}

func TestMeasureSplitLongBlankRuns(t *testing.T) {
	var classes classifier
	f, err := readFile(t.Context(), []byte(strings.Repeat("\n", 2*maxBlanks+3)), 0, &classes)
	if err != nil {
		t.Fatal(err)
	}
	middle := measureSplit(&f, maxBlanks+1)
	if middle != (splitMeasurement{indent: -1, preBlank: maxBlanks, preIndent: 0,
		postBlank: maxBlanks, postIndent: 0}) {
		t.Fatalf("middle of blank run: %+v", middle)
	}
	end := measureSplit(&f, len(f.recs))
	if !end.endOfFile || end.preBlank != maxBlanks || end.preIndent != 0 || end.postIndent != -1 {
		t.Fatalf("end of blank run: %+v", end)
	}
}

func TestSplitScoreBlankBeforeOutdent(t *testing.T) {
	var score splitScore
	score.add(splitMeasurement{indent: 0, preIndent: 4, postIndent: 8, preBlank: 1})
	if score != (splitScore{effectiveIndent: 0, penalty: -13}) {
		t.Fatalf("outdent after blank: %+v", score)
	}
}

func TestChangeGroupTraversal(t *testing.T) {
	f := myersTestFile([]int{1, 2, 3, 4, 5})
	f.setChanged(1, true)
	f.setChanged(2, true)
	f.setChanged(4, true)
	g := firstGroup(&f)
	var groups []changeGroup
	for {
		groups = append(groups, g)
		if !g.next(&f) {
			break
		}
	}
	for i := len(groups) - 1; i >= 0; i-- {
		if g != groups[i] {
			t.Fatalf("backward group = %+v, want %+v", g, groups[i])
		}
		if got := g.previous(&f); got != (i > 0) {
			t.Fatalf("previous at group %d = %v", i, got)
		}
	}
}

func TestChangedGroupCount(t *testing.T) {
	f := myersTestFile([]int{0, 1, 2, 3, 4})
	for _, op := range []struct {
		line    int
		changed bool
		groups  int
	}{
		{0, true, 1}, {2, true, 2}, {1, true, 1}, {1, false, 2},
		{4, true, 3}, {2, false, 2}, {0, false, 1}, {4, false, 0},
		{4, false, 0}, {3, true, 1}, {4, true, 1}, {3, false, 1}, {4, false, 0},
	} {
		f.setChanged(op.line, op.changed)
		if f.groups != op.groups {
			t.Fatalf("line %d, changed %v: %d groups, want %d", op.line, op.changed, f.groups, op.groups)
		}
	}
}

func TestCompactHistogramRediff(t *testing.T) {
	for _, swapped := range []bool{false, true} {
		ctx := context.Background()
		var classes classifier
		a, err := readFile(ctx, []byte("a\na\na\n"), 0, &classes)
		if err != nil {
			t.Fatal(err)
		}
		b, err := readFile(ctx, []byte("x\na\ny\n"), 0, &classes)
		if err != nil {
			t.Fatal(err)
		}
		if swapped {
			a, b = b, a
		}
		e := environment{a: a, b: b, ctx: ctx, opts: Options{Algorithm: Histogram}}
		for _, f := range []*file{&e.a, &e.b} {
			f.setChanged(0, true)
			f.setChanged(2, true)
		}
		if err := e.compact(); err != nil {
			t.Fatalf("swapped=%v: %v", swapped, err)
		}
		if e.opts.Algorithm != Histogram {
			t.Fatal("compaction changed the algorithm option")
		}
		ai, bi := 0, 0
		for _, c := range e.script() {
			if c.OldStart-ai != c.NewStart-bi {
				t.Fatal("unequal context lengths after histogram rediff")
			}
			for ai < c.OldStart {
				if e.a.recs[ai].id != e.b.recs[bi].id {
					t.Fatal("unequal context after histogram rediff")
				}
				ai++
				bi++
			}
			ai += c.OldLines
			bi += c.NewLines
		}
		for ai < len(e.a.recs) && bi < len(e.b.recs) {
			if e.a.recs[ai].id != e.b.recs[bi].id {
				t.Fatal("unequal final context after histogram rediff")
			}
			ai++
			bi++
		}
		if ai != len(e.a.recs) || bi != len(e.b.recs) {
			t.Fatal("incomplete script after histogram rediff")
		}
	}
}
