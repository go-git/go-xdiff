package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-git/go-xdiff/benchmarks/internal/corpus"
)

func TestReadSamples(t *testing.T) {
	input := `goos: darwin
BenchmarkCompare/SmallEdit/GoXDiff-1 100 1200 ns/op 10 MB/s 2 edit-lines/op 512 B/op 8 allocs/op
BenchmarkCompare/SmallEdit/GitC 200 300 ns/op
BenchmarkStringAdapter/SmallEdit-1 90 1400 ns/op 1024 B/op 10 allocs/op
PASS
`
	results, err := readSamples(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	goSample := results["SmallEdit/GoXDiff"][0]
	if goSample.ns != 1200 || goSample.bytes != 512 || goSample.allocs != 8 || goSample.lines != 2 {
		t.Fatalf("Go sample = %+v", goSample)
	}
	if results["SmallEdit/GitC"][0].hasMemory {
		t.Fatal("C allocation data must remain unavailable")
	}
	if results["SmallEdit/StringAdapter"][0].ns != 1400 {
		t.Fatal("adapter CPU suffix was not removed")
	}
}

func TestReadSamplesRejectsInvalidMetrics(t *testing.T) {
	for _, line := range []string{
		"BenchmarkCompare/A/GitC 0 100 ns/op",
		"BenchmarkCompare/A/GitC 1 NaN ns/op",
		"BenchmarkCompare/A/GitC 1 0 ns/op",
		"BenchmarkCompare/A/GoXDiff 1 100 ns/op 10 B/op",
		"BenchmarkCompare/A/GitC 1 100",
	} {
		if _, err := readSamples(strings.NewReader(line)); err == nil {
			t.Errorf("accepted %q", line)
		}
	}
}

func TestReportRequiresCompleteSamples(t *testing.T) {
	var output bytes.Buffer
	if err := report(&output, []corpus.Manifest{{Name: "A"}}, nil, 5); err == nil {
		t.Fatal("accepted missing benchmark samples")
	}
	values := []sample{{ns: 300}, {ns: 100}, {ns: 200}, {ns: 10000}, {ns: 400}}
	if got := median(values, func(s sample) float64 { return s.ns }); got != 300 {
		t.Fatalf("median = %v", got)
	}
}

func TestReportGoDiff(t *testing.T) {
	input := `BenchmarkCompare/A/GitC 1 100 ns/op
BenchmarkCompare/A/GoXDiff 1 200 ns/op 2 edit-lines/op 64 B/op 1 allocs/op
BenchmarkCompare/A/GoUDiff 1 300 ns/op 2 edit-lines/op 128 B/op 2 allocs/op
BenchmarkCompare/A/GoDiff-1 1 400 ns/op 2 edit-lines/op 256 B/op 3 allocs/op
BenchmarkStringAdapter/A-1 1 500 ns/op 512 B/op 4 allocs/op
`
	samples, err := readSamples(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := report(&output, []corpus.Manifest{{Name: "A"}}, samples, 1); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "| go-diff |") {
		t.Fatal("report does not label the go-diff column")
	}
}
