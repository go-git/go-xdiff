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
	e := environment{
		a: myersTestFile([]int{1}), b: myersTestFile(nil),
		ctx: context.Background(),
	}
	if err := e.compact(); err == nil {
		t.Fatal("compaction accepted unequal unchanged groups")
	}
}

func TestCompactHistogramRediff(t *testing.T) {
	for _, swapped := range []bool{false, true} {
		ctx := context.Background()
		classes := make(map[string]int)
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
