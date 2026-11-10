package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/go-git/go-xdiff/benchmarks/internal/corpus"
)

type sample struct {
	ns, bytes, allocs, lines float64
	hasMemory, hasLines      bool
}

func readSamples(r io.Reader) (map[string][]sample, error) {
	results := make(map[string][]sample)
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "Benchmark") {
			continue
		}
		parts := strings.Split(fields[0], "/")
		var key string
		switch {
		case len(parts) == 3 && parts[0] == "BenchmarkCompare":
			engine := parts[2]
			if i := strings.LastIndexByte(engine, '-'); i >= 0 {
				if _, err := strconv.Atoi(engine[i+1:]); err == nil {
					engine = engine[:i]
				}
			}
			key = parts[1] + "/" + engine
		case len(parts) == 2 && parts[0] == "BenchmarkStringAdapter":
			name := parts[1]
			if i := strings.LastIndexByte(name, '-'); i >= 0 {
				if _, err := strconv.Atoi(name[i+1:]); err == nil {
					name = name[:i]
				}
			}
			key = name + "/StringAdapter"
		default:
			return nil, fmt.Errorf("unrecognized benchmark name %q", fields[0])
		}
		if len(fields) < 4 || len(fields)%2 != 0 {
			return nil, fmt.Errorf("invalid benchmark line %q", scanner.Text())
		}
		n, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil || n == 0 {
			return nil, fmt.Errorf("invalid iteration count %q", fields[1])
		}
		var s sample
		hasNS, hasBytes, hasAllocs := false, false, false
		for i := 2; i < len(fields); i += 2 {
			value, err := strconv.ParseFloat(fields[i], 64)
			if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("invalid metric %q", fields[i])
			}
			switch fields[i+1] {
			case "ns/op":
				s.ns, hasNS = value, true
			case "B/op":
				s.bytes, hasBytes = value, true
			case "allocs/op":
				s.allocs, hasAllocs = value, true
			case "edit-lines/op":
				s.lines, s.hasLines = value, true
			case "MB/s":
			default:
				return nil, fmt.Errorf("unknown benchmark metric %q", fields[i+1])
			}
		}
		if !hasNS || s.ns <= 0 || hasBytes != hasAllocs {
			return nil, fmt.Errorf("incomplete benchmark metrics for %s", key)
		}
		s.hasMemory = hasBytes
		results[key] = append(results[key], s)
	}
	return results, scanner.Err()
}

func median(samples []sample, field func(sample) float64) float64 {
	values := make([]float64, len(samples))
	for i, s := range samples {
		values[i] = field(s)
	}
	slices.Sort(values)
	middle := len(values) / 2
	if len(values)%2 == 0 {
		return (values[middle-1] + values[middle]) / 2
	}
	return values[middle]
}

func report(output io.Writer, manifest []corpus.Manifest, samples map[string][]sample, count int) error {
	engines := []string{"GitC", "GoXDiff", "GoUDiff", "GoDiff", "StringAdapter"}
	for _, c := range manifest {
		for _, engine := range engines {
			key := c.Name + "/" + engine
			group := samples[key]
			if len(group) != count {
				return fmt.Errorf("%s: got %d samples, want %d", key, len(group), count)
			}
			for _, s := range group {
				if engine != "GitC" && !s.hasMemory {
					return fmt.Errorf("%s: missing allocation metrics", key)
				}
				if engine != "GitC" && engine != "StringAdapter" && !s.hasLines {
					return fmt.Errorf("%s: missing edit-volume metric", key)
				}
			}
		}
	}
	ns := func(key string) float64 {
		return median(samples[key], func(s sample) float64 { return s.ns }) / 1000
	}
	var buffer bytes.Buffer
	w := &buffer
	fmt.Fprintf(w, "# Comparative diff benchmarks\n\nMedian of %d samples per engine and workload. Times are microseconds per operation.\n\n", count)
	fmt.Fprintln(w, "| Workload | Input bytes, old + new | Git C | go-xdiff | go-udiff Lines | go-diff |")
	fmt.Fprintln(w, "|---|---:|---:|---:|---:|---:|")
	for _, c := range manifest {
		fmt.Fprintf(w, "| %s | %d | %.2f | %.2f | %.2f | %.2f |\n", c.Name, c.OldBytes+c.NewBytes,
			ns(c.Name+"/GitC"), ns(c.Name+"/GoXDiff"), ns(c.Name+"/GoUDiff"), ns(c.Name+"/GoDiff"))
	}
	fmt.Fprint(w, "\n## Go allocations\n\n")
	fmt.Fprint(w, "Each cell reports KiB allocated per operation / allocations per operation. C allocation data is not collected.\n\n")
	fmt.Fprintln(w, "| Workload | go-xdiff | go-udiff Lines | go-diff |")
	fmt.Fprintln(w, "|---|---:|---:|---:|")
	for _, c := range manifest {
		fmt.Fprintf(w, "| %s", c.Name)
		for _, engine := range engines[1:4] {
			group := samples[c.Name+"/"+engine]
			nbytes := median(group, func(s sample) float64 { return s.bytes })
			nallocs := median(group, func(s sample) float64 { return s.allocs })
			fmt.Fprintf(w, " | %.1f / %.0f", nbytes/1024, nallocs)
		}
		fmt.Fprintln(w, " |")
	}
	fmt.Fprint(w, "\n## go-git string input adapter\n\n")
	fmt.Fprint(w, "The native go-xdiff baseline receives existing byte slices. The adapter includes conversion of both existing strings into byte slices.\n\n")
	fmt.Fprintln(w, "| Workload | Native bytes, us/op | From strings, us/op | From strings, KiB/op | From strings, allocs/op |")
	fmt.Fprintln(w, "|---|---:|---:|---:|---:|")
	for _, c := range manifest {
		group := samples[c.Name+"/StringAdapter"]
		fmt.Fprintf(w, "| %s | %.2f | %.2f | %.1f | %.0f |\n", c.Name, ns(c.Name+"/GoXDiff"),
			ns(c.Name+"/StringAdapter"), median(group, func(s sample) float64 { return s.bytes })/1024,
			median(group, func(s sample) float64 { return s.allocs }))
	}
	fmt.Fprint(w, "\n## Edit volume\n\n")
	fmt.Fprint(w, "Changed lines count deleted plus inserted lines. The validation suite checks reconstruction and exact go-xdiff/Git C script parity.\n\n")
	fmt.Fprintln(w, "| Workload | go-xdiff / Git C | go-udiff Lines | go-diff |")
	fmt.Fprintln(w, "|---|---:|---:|---:|")
	for _, c := range manifest {
		fmt.Fprintf(w, "| %s", c.Name)
		for _, engine := range engines[1:4] {
			group := samples[c.Name+"/"+engine]
			value := group[0].lines
			for _, s := range group {
				if s.lines != value {
					return fmt.Errorf("%s/%s: edit volume changed between samples", c.Name, engine)
				}
			}
			fmt.Fprintf(w, " | %.0f", value)
		}
		fmt.Fprintln(w, " |")
	}
	_, err := io.Copy(output, &buffer)
	return err
}

func run() error {
	dir := flag.String("dir", "", "directory with go.txt, c.txt, and fixtures/manifest.json")
	count := flag.Int("count", 5, "expected number of samples")
	flag.Parse()
	if *dir == "" || *count <= 0 || flag.NArg() != 0 {
		return fmt.Errorf("usage: report -dir DIRECTORY [-count N]")
	}
	manifestData, err := os.ReadFile(filepath.Join(*dir, "fixtures", "manifest.json"))
	if err != nil {
		return err
	}
	var manifest []corpus.Manifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return err
	}
	if len(manifest) == 0 {
		return fmt.Errorf("empty fixture manifest")
	}
	results := make(map[string][]sample)
	for _, name := range []string{"go.txt", "c.txt"} {
		f, err := os.Open(filepath.Join(*dir, name))
		if err != nil {
			return err
		}
		samples, parseErr := readSamples(f)
		closeErr := f.Close()
		if parseErr != nil {
			return fmt.Errorf("%s: %w", name, parseErr)
		}
		if closeErr != nil {
			return closeErr
		}
		for key, group := range samples {
			results[key] = append(results[key], group...)
		}
	}
	return report(os.Stdout, manifest, results, *count)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
