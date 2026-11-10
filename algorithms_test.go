package xdiff

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"reflect"
	"slices"
	"testing"
)

func TestAlgorithms(t *testing.T) {
	pairs := [][2]string{
		{"", ""}, {"", "a\n"}, {"a\n", ""}, {"a", "a\n"},
		{"a\nb\nc\n", "a\nB\nc\n"},
		{"a\nb\nc\n", "c\nb\na\n"},
		{"x\nx\na\n", "x\na\nx\n"},
		{"a\nb\na\nb\n", "b\na\nb\na\n"},
		{"a\nx\nb\nx\nc\n", "a\nx\nc\nx\nb\n"},
	}
	for _, algorithm := range []Algorithm{Patience, Histogram} {
		for i, pair := range pairs {
			t.Run(fmt.Sprintf("%d/%d", algorithm, i), func(t *testing.T) {
				opts := Options{Algorithm: algorithm}
				a, b := []byte(pair[0]), []byte(pair[1])
				result, err := Diff(context.Background(), a, b, &opts)
				if err != nil {
					t.Fatal(err)
				}
				checkChunks(t, a, b, result)
				if i == 4 && !reflect.DeepEqual(result.Changes(), []Change{{1, 1, 1, 1}}) {
					t.Fatalf("replacement: %+v", result.Changes())
				}
			})
		}
	}
}

func TestPatienceAnchors(t *testing.T) {
	a, b := []byte("a\nb\nc\n"), []byte("c\nb\na\n")
	for _, tc := range []struct {
		anchors []string
		want    []Change
	}{
		{nil, []Change{{0, 2, 0, 0}, {3, 0, 1, 2}}},
		{[]string{"a"}, []Change{{0, 0, 0, 2}, {1, 2, 3, 0}}},
		{[]string{"a\x00ignored"}, []Change{{0, 0, 0, 2}, {1, 2, 3, 0}}},
		{[]string{"b"}, []Change{{0, 1, 0, 1}, {2, 1, 2, 1}}},
		{[]string{"not present"}, []Change{{0, 2, 0, 0}, {3, 0, 1, 2}}},
		{[]string{""}, []Change{{0, 0, 0, 2}, {1, 2, 3, 0}}},
	} {
		opts := Options{Algorithm: Patience, Anchors: tc.anchors}
		before := append([]string(nil), opts.Anchors...)
		r, err := Diff(context.Background(), a, b, &opts)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(r.Changes(), tc.want) {
			t.Fatalf("anchors %q: got %+v, want %+v", tc.anchors, r.Changes(), tc.want)
		}
		if !reflect.DeepEqual(opts.Anchors, before) {
			t.Fatal("anchors changed")
		}
	}
}

func TestAlgorithmsReference(t *testing.T) {
	ref := oracle(t)
	rng := rand.New(rand.NewSource(20261109))
	for _, algorithm := range []Algorithm{Patience, Histogram} {
		for i := 0; i < 500; i++ {
			a, b := randomText(rng, rng.Intn(80)), randomText(rng, rng.Intn(80))
			opts := Options{Algorithm: algorithm, IndentHeuristic: i%2 == 0}
			if i%5 != 0 {
				opts.Whitespace = Whitespace(1 << uint(i%4))
			}
			if algorithm == Patience && i%3 == 0 {
				opts.Anchors = []string{"a", "f()", "  "}
			}
			compareScript(t, ref, a, b, opts)
		}
		for i := 0; i < 12; i++ {
			compareScript(t, ref, randomText(rng, 1000), randomText(rng, 1200),
				Options{Algorithm: algorithm, IndentHeuristic: i%2 == 0})
		}
		for _, pair := range [][2]string{
			{"", "a\n"}, {"a\n", ""}, {"same", "same"}, {"a\n", "b\n"},
			{"x\na\nb\ny\nx\nc\nd\ny\n", "x\nc\nd\ny\nx\na\nb\ny\n"},
			{string(bytes.Repeat([]byte("a\nb\n"), 80)), string(bytes.Repeat([]byte("b\na\n"), 90))},
		} {
			compareScript(t, ref, []byte(pair[0]), []byte(pair[1]), Options{Algorithm: algorithm})
		}
	}
}

func TestPatienceAnchorsReference(t *testing.T) {
	ref := oracle(t)
	for _, anchors := range [][]string{
		nil, {"a"}, {"b"}, {"a", "b"}, {""}, {"no match"}, {"a\nb"},
	} {
		for _, pair := range [][2]string{
			{"a\nb\nc\n", "c\nb\na\n"},
			{"a\nb\nc\nd\ne\n", "c\nd\nb\na\ne\n"},
			{"a\nb\nx\na\nc\n", "x\nb\na\nc\na\n"},
			{"a\nb\nx\n", "x\na\nb\n"},
		} {
			compareScript(t, ref, []byte(pair[0]), []byte(pair[1]),
				Options{Algorithm: Patience, Anchors: anchors})
		}
	}
	for _, mode := range []Whitespace{IgnoreAllSpace, IgnoreSpaceChange, IgnoreSpaceAtEOL, IgnoreCRAtEOL} {
		for _, anchors := range [][]string{{" a"}, {"a\r"}, {"a"}, {"b"}} {
			compareScript(t, ref, []byte(" a \nb\nc\n"), []byte("c\nb\na\r\n"),
				Options{Algorithm: Patience, Anchors: anchors, Whitespace: mode})
		}
	}
}

func TestHistogramRarityAndTiesReference(t *testing.T) {
	ref := oracle(t)
	rng := rand.New(rand.NewSource(64))
	for i := 0; i < 150; i++ {
		var a, b []byte
		for line := 0; line < 150; line++ {
			a = fmt.Appendf(a, "%d\n", rng.Intn(100))
			b = fmt.Appendf(b, "%d\n", rng.Intn(100))
		}
		compareScript(t, ref, a, b, Options{Algorithm: Histogram, IndentHeuristic: i%2 == 0})
	}
	a, b := histogramCollisionText(histogramMaxChain)
	compareScript(t, ref, a, b, Options{Algorithm: Histogram})
}

func histogramCollisionText(count int) ([]byte, []byte) {
	var prefix []byte
	for i := 0; i < count*128; i++ {
		prefix = fmt.Appendf(prefix, "%d\n", i)
	}
	a, b := bytes.Clone(prefix), bytes.Clone(prefix)
	for i := 0; i < count; i++ {
		// These class IDs occupy one XDL_HASHLONG bucket in the tail.
		a = fmt.Appendf(a, "%d\n", i*128+(-i&127))
		j := count - i - 1
		b = fmt.Appendf(b, "%d\n", j*128+(-j&127))
	}
	return a, b
}

func TestHistogramChainLimit(t *testing.T) {
	a, b := histogramCollisionText(histogramMaxChain + 1)
	_, err := Diff(context.Background(), a, b, &Options{Algorithm: Histogram})
	if err == nil {
		t.Fatal("accepted a hash chain with 65 distinct records")
	}
}

func TestMyersHeuristicsReference(t *testing.T) {
	ref := oracle(t)
	records := func(prefix string, count int) []byte {
		var data []byte
		for i := range count {
			data = fmt.Appendf(data, "%s%d\n", prefix, i)
		}
		return data
	}
	left, right := records("left", 16500), records("right", 16500)
	middle, moved := records("middle", 300), records("moved", 300)
	old := bytes.Join([][]byte{left, middle, right, moved}, nil)
	new := bytes.Join([][]byte{moved, left, right, middle}, nil)
	reverse := func(data []byte) []byte {
		lines := bytes.SplitAfter(data, []byte{'\n'})
		slices.Reverse(lines)
		return bytes.Join(lines, nil)
	}
	for _, pair := range [][2][]byte{{old, new}, {new, old}, {reverse(old), reverse(new)}, {reverse(new), reverse(old)}} {
		for _, minimal := range []bool{false, true} {
			compareScript(t, ref, pair[0], pair[1], Options{Minimal: minimal})
		}
	}
	for _, pair := range [][2][]byte{
		{[]byte("a\nb\n"), append([]byte("b\n"), bytes.Repeat([]byte("a\n"), 1024)...)},
		{append([]byte("b\n"), bytes.Repeat([]byte("a\n"), 1024)...), []byte("a\nb\n")},
	} {
		compareScript(t, ref, pair[0], pair[1], Options{})
	}
}

type algorithmCancelContext struct {
	context.Context
	remaining int
}

func (c *algorithmCancelContext) Err() error {
	c.remaining--
	if c.remaining <= 0 {
		return context.Canceled
	}
	return nil
}

func TestAlgorithmCancellation(t *testing.T) {
	for _, algorithm := range []Algorithm{Patience, Histogram} {
		for _, limit := range []int{1, 50, 250, 450, 700, 1050} {
			a, b := bytes.Repeat([]byte("a\nb\n"), 100), bytes.Repeat([]byte("b\na\n"), 100)
			e, err := prepare(context.Background(), a, b, Options{Algorithm: algorithm})
			if err != nil {
				t.Fatal(err)
			}
			e.ctx = &algorithmCancelContext{Context: context.Background(), remaining: limit}
			if algorithm == Patience {
				err = e.patience()
			} else {
				err = e.histogram()
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("algorithm %d, limit %d: %v", algorithm, limit, err)
			}
		}
	}
}

func ExampleDiff_patience() {
	opts := Options{Algorithm: Patience}
	result, err := Diff(context.Background(), []byte("a\nb\nc\n"), []byte("c\nb\na\n"), &opts)
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Changes())
	// Output: [{0 2 0 0} {3 0 1 2}]
}

func ExampleDiff_histogram() {
	opts := Options{Algorithm: Histogram}
	result, err := Diff(context.Background(), []byte("one\ntwo\n"), []byte("one\nthree\n"), &opts)
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Changes())
	// Output: [{1 1 1 1}]
}

func ExampleDiff_anchors() {
	opts := Options{Algorithm: Patience, Anchors: []string{"a"}}
	result, err := Diff(context.Background(), []byte("a\nb\nc\n"), []byte("c\nb\na\n"), &opts)
	if err != nil {
		panic(err)
	}
	fmt.Println(result.Changes())
	// Output: [{0 0 0 2} {1 2 3 0}]
}
