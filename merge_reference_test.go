package xdiff

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/go-git/go-xdiff/internal/testref"
)

func compareMerge(t *testing.T, ref *testref.Oracle, base, ours, theirs []byte, opts MergeOptions) {
	t.Helper()
	result, err := Merge(context.Background(), base, ours, theirs, &opts)
	if err != nil {
		t.Fatal(err)
	}
	expected, count := ref.MergeCount(t, base, ours, theirs, testref.MergeOptions{
		Flags: referenceFlags(opts.Diff), Level: int(opts.Level), Favor: int(opts.Favor),
		Style: int(opts.Style), MarkerSize: opts.MarkerSize,
		Ancestor: opts.AncestorLabel, Ours: opts.OursLabel, Theirs: opts.TheirsLabel,
	})
	if !bytes.Equal(result.Content, expected) || result.Conflicts != count {
		t.Fatalf("merge mismatch opts=%+v\nbase=%q\nours=%q\ntheirs=%q\ngot(%d)=%q\nwant(%d)=%q",
			opts, base, ours, theirs, result.Conflicts, result.Content, count, expected)
	}
}

func mutateText(rng *rand.Rand, base []byte) []byte {
	lines := bytes.SplitAfter(base, []byte{'\n'})
	if len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	for range 1 + rng.Intn(4) {
		i := rng.Intn(len(lines) + 1)
		switch rng.Intn(3) {
		case 0:
			if i < len(lines) {
				lines = append(lines[:i], lines[i+1:]...)
			}
		case 1:
			line := []byte(fmt.Sprintf("new%d\n", rng.Intn(5)))
			lines = append(lines, nil)
			copy(lines[i+1:], lines[i:])
			lines[i] = line
		case 2:
			if i < len(lines) {
				lines[i] = []byte(fmt.Sprintf("changed%d\n", rng.Intn(5)))
			}
		}
	}
	return bytes.Join(lines, nil)
}

func TestMergeReference(t *testing.T) {
	ref := oracle(t)
	cases := [][3]string{
		{"", "", ""}, {"", "a", "b"}, {"a\n", "a\n", "b\n"},
		{"a\nb\nc\n", "A\nb\nc\n", "a\nb\nC\n"},
		{"a\nb\nc\n", "a\nB\nc\n", "a\nX\nc\n"},
		{"a\nb\nc\n", "a\nx\nb\nc\n", "a\nb\ny\nc\n"},
		{"a\nb\nc\n", "a\nc\n", "a\nB\nc\n"},
		{"a", "b", "c"}, {"a\r\nb\r\n", "a\r\nB\r\n", "a\r\nC\r\n"},
		{"a\r\n", "b\r\n", "c"}, {"a\n", "", "a\nb\n"},
		{"base\n", "a\nsame\nb\n", "c\nsame\nd\n"},
		{"base\n", "a\n!\nb\n", "c\n!\nd\n"},
		{"base\n", "same\nours\nend\n", "same\ntheirs\nend\n"},
		{"\xff\x00\n", "\xff\x00\nx", "\xfe\x00\n"},
	}
	for i, triple := range cases {
		for level := MergeMinimal; level <= MergeZealousAlnum; level++ {
			for style := ConflictMerge; style <= ConflictZDiff3; style++ {
				opts := MergeOptions{Level: level, Style: style, MarkerSize: 3 + i%5,
					AncestorLabel: "base", OursLabel: "ours", TheirsLabel: "theirs"}
				if i%2 == 0 {
					opts.MarkerSize = 0
					opts.AncestorLabel, opts.OursLabel, opts.TheirsLabel = "", "", ""
				}
				opts.Favor = MergeFavor(i % 4)
				compareMerge(t, ref, []byte(triple[0]), []byte(triple[1]), []byte(triple[2]), opts)
			}
		}
	}
	rng := rand.New(rand.NewSource(617))
	for i := 0; i < 400; i++ {
		base := randomText(rng, rng.Intn(35))
		ours, theirs := mutateText(rng, base), mutateText(rng, base)
		if i%7 == 0 {
			ours, theirs = randomText(rng, rng.Intn(25)), randomText(rng, rng.Intn(25))
		}
		opts := MergeOptions{Level: MergeLevel(i % 4), Style: ConflictStyle(i % 3),
			Favor: MergeFavor((i / 3) % 4), MarkerSize: i % 9,
			Diff: Options{Algorithm: Algorithm(i % 3), IndentHeuristic: i%2 == 0}}
		if i%5 != 0 {
			opts.Diff.Whitespace = Whitespace(1 << uint((i/5)%4))
		}
		if i%3 == 0 {
			opts.AncestorLabel, opts.OursLabel, opts.TheirsLabel = "original", "left", "right"
		}
		compareMerge(t, ref, base, ours, theirs, opts)
	}
}

func FuzzMerge(f *testing.F) {
	f.Add([]byte("a\nb\n"), []byte("a\nB\n"), []byte("A\nb\n"), uint8(0))
	f.Add([]byte{}, []byte{0xff, '\n'}, []byte{0, '\r', '\n'}, uint8(255))
	f.Fuzz(func(t *testing.T, base, ours, theirs []byte, flags uint8) {
		if len(base)+len(ours)+len(theirs) > 10000 {
			t.Skip()
		}
		opts := MergeOptions{Level: MergeLevel(flags % 4), Style: ConflictStyle((flags >> 2) % 3),
			Favor: MergeFavor((flags >> 4) % 4), MarkerSize: int(flags % 9),
			Diff: Options{Algorithm: Algorithm((flags >> 6) % 3)}}
		result, err := Merge(context.Background(), base, ours, theirs, &opts)
		if err != nil {
			t.Fatal(err)
		}
		if result.Conflicts < 0 {
			t.Fatalf("negative conflicts: %d", result.Conflicts)
		}
		if opts.Favor != FavorConflict && result.Conflicts != 0 {
			t.Fatalf("favored merge has %d unresolved conflicts", result.Conflicts)
		}
		identicalOpts := opts
		// Minimal mode deliberately permits false conflicts.
		identicalOpts.Level, identicalOpts.Favor = MergeEager, FavorConflict
		identical, err := Merge(context.Background(), base, ours, ours, &identicalOpts)
		if err != nil {
			t.Fatal(err)
		}
		if identical.Conflicts != 0 || !bytes.Equal(identical.Content, ours) {
			t.Fatalf("identical sides: %+v", identical)
		}
	})
}
