package xdiff

import (
	"context"
	"errors"
	"testing"
)

func myersTestFile(ids []int) file {
	f := file{changed: make([]bool, len(ids)+2)}
	for i, id := range ids {
		f.recs = append(f.recs, record{id: id})
		f.refs = append(f.refs, i)
	}
	return f
}

func TestCleanMultimatch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action []uint8
		index  int
		want   bool
	}{
		{"first record", []uint8{2, 0, 0, 0, 0}, 0, false},
		{"last record", []uint8{0, 0, 0, 0, 2}, 4, false},
		{"discard", []uint8{0, 0, 0, 0, 2, 0, 0, 0, 0}, 4, true},
		{"threshold", []uint8{0, 0, 0, 2, 0, 0, 0}, 3, false},
		{"mixed multimatches", []uint8{0, 0, 0, 2, 2, 0, 0, 0, 0, 0, 0, 0}, 3, true},
		{"no discard before", []uint8{2, 2, 2, 2, 2, 0, 0, 0, 0}, 4, false},
		{"no discard after", []uint8{0, 0, 0, 0, 2, 2, 2, 2, 2}, 4, false},
		{"keep before", []uint8{0, 0, 0, 0, 1, 2, 0, 0, 0, 0}, 5, false},
		{"keep after", []uint8{0, 0, 0, 0, 2, 1, 0, 0, 0, 0}, 4, false},
		{"keep bounds", []uint8{2, 2, 2, 2, 2, 1, 0, 0, 0, 0, 2, 0, 0, 0, 0, 1, 2, 2, 2, 2, 2}, 10, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanMultimatch(tc.action, tc.index); got != tc.want {
				t.Fatalf("cleanMultimatch(%v, %d) = %v, want %v", tc.action, tc.index, got, tc.want)
			}
		})
	}
	t.Run("window", func(t *testing.T) {
		action := make([]uint8, 203)
		for i := range 49 {
			action[i] = 2
		}
		action[101], action[202] = 2, 2
		if !cleanMultimatch(action, 101) {
			t.Fatal("records outside the 100-record window changed the decision")
		}
	})
}

func TestMyersMinimalShortSequences(t *testing.T) {
	var sequences [][]int
	for length := 0; length <= 5; length++ {
		for mask := 0; mask < 1<<length; mask++ {
			sequence := make([]int, length)
			for i := range sequence {
				sequence[i] = mask >> i & 1
			}
			sequences = append(sequences, sequence)
		}
	}
	for _, a := range sequences {
		for _, b := range sequences {
			e := environment{
				a: myersTestFile(a), b: myersTestFile(b),
				ctx: context.Background(), opts: Options{Minimal: true},
			}
			if err := e.myers(); err != nil {
				t.Fatalf("myers(%v, %v): %v", a, b, err)
			}
			if err := e.compact(); err != nil {
				t.Fatalf("compact(%v, %v): %v", a, b, err)
			}
			dp := make([][]int, len(a)+1)
			for i := range dp {
				dp[i] = make([]int, len(b)+1)
				for j := range dp[i] {
					switch {
					case i == 0:
						dp[i][j] = j
					case j == 0:
						dp[i][j] = i
					case a[i-1] == b[j-1]:
						dp[i][j] = dp[i-1][j-1]
					default:
						dp[i][j] = 1 + min(dp[i-1][j], dp[i][j-1])
					}
				}
			}
			cost, ai, bi := 0, 0, 0
			checkEqual := func(aEnd, bEnd int) {
				t.Helper()
				if aEnd-ai != bEnd-bi {
					t.Fatalf("unequal context lengths for %v -> %v", a, b)
				}
				for ai < aEnd {
					if a[ai] != b[bi] {
						t.Fatalf("unequal context for %v -> %v", a, b)
					}
					ai++
					bi++
				}
			}
			for _, change := range e.script() {
				checkEqual(change.OldStart, change.NewStart)
				cost += change.OldLines + change.NewLines
				ai += change.OldLines
				bi += change.NewLines
			}
			checkEqual(len(a), len(b))
			if cost != dp[len(a)][len(b)] {
				t.Fatalf("nonminimal script for %v -> %v: cost %d, want %d", a, b, cost, dp[len(a)][len(b)])
			}
		}
	}
}

func TestMyersCancellationAndInvalidState(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	e := environment{ctx: ctx}
	if err := e.myers(); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Myers: %v", err)
	}
	e.ctx = t.Context()
	e.a, e.b = myersTestFile([]int{1}), myersTestFile(nil)
	e.a.refs[0] = 1
	if err := e.myers(); err == nil {
		t.Fatal("Myers accepted an invalid reference")
	}
	e.a = myersTestFile(nil)
	e.a.changed[0] = true
	if err := e.compact(); err == nil {
		t.Fatal("compaction accepted a changed sentinel")
	}
	for _, invalid := range []file{
		{recs: []record{{id: 0}}, changed: make([]bool, 2)},
		{changed: []bool{false, true}},
	} {
		for _, other := range []bool{false, true} {
			e.a, e.b = myersTestFile(nil), myersTestFile(nil)
			if other {
				e.b = invalid
			} else {
				e.a = invalid
			}
			if err := e.myers(); err == nil {
				t.Fatal("Myers accepted an invalid change array")
			}
			if err := e.compact(); err == nil {
				t.Fatal("compaction accepted an invalid change array")
			}
		}
	}
}
