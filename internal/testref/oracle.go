// Package testref runs the pinned C xdiff implementation for explicit test use.
// It has no dependency on the Go implementation and does not use CGO.
package testref

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// DiffOptions uses the numeric constants from upstream/xdiff.h.
// Zero context means no context, not the Git default of three lines.
type DiffOptions struct {
	Flags     uint64
	Context   int
	Interhunk int
	EmitFlags uint64
	Anchors   []string
	// IgnorePatterns uses POSIX ERE with REG_NEWLINE, as Git's -I option does.
	IgnorePatterns []string
}

// MergeOptions uses the numeric constants from upstream/xdiff.h.
// Empty labels omit labels. A zero marker size selects the upstream default.
type MergeOptions struct {
	Flags                           uint64
	Level, Favor, Style, MarkerSize int
	Ancestor, Ours, Theirs          string
}

// Change contains zero-based line offsets and line counts before ignore filters.
type Change struct {
	OldStart, OldCount, NewStart, NewCount int
}

// Oracle owns a compiled runner until its test and all subtests finish.
// Methods may run concurrently. Do not retain an Oracle beyond its test.
type Oracle struct {
	binary string
	dir    string
}

// New compiles an independent runner. It skips only when cc is absent.
// Compiler and operation failures fail the test, with no fallback.
// All work files stay below the current directory and use test cleanup.
func New(t testing.TB) *Oracle {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skipf("C reference oracle requires cc: %v", err)
		return nil
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate C reference source")
	}
	source, err := filepath.Abs(filepath.Dir(file))
	if err != nil {
		t.Fatal(err)
	}
	dir := workdir(t, ".")
	o := &Oracle{binary: filepath.Join(dir, "oracle"), dir: dir}
	args := []string{"-std=c99", "-O2", "-Wall", "-Wextra",
		"-I" + filepath.Join(source, "runner"),
		"-I" + filepath.Join(source, "upstream"),
		filepath.Join(source, "runner", "main.c")}
	sources, err := filepath.Glob(filepath.Join(source, "upstream", "*.c"))
	if err != nil {
		t.Fatal(err)
	}
	args = append(args, sources...)
	args = append(args, "-o", o.binary)
	cmd := exec.Command(cc, args...)
	cmd.Env = append(os.Environ(), "TMPDIR="+dir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("compile C reference: %v\n%s", err, output)
	}
	return o
}

func workdir(t testing.TB, parent string) string {
	t.Helper()
	dir, err := os.MkdirTemp(parent, ".testref-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove C reference work files: %v", err)
		}
	})
	absolute, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return absolute
}

func (o *Oracle) run(t testing.TB, args []string, inputs ...[]byte) ([]byte, bool) {
	t.Helper()
	return o.runTail(t, args, nil, inputs...)
}

func (o *Oracle) runTail(t testing.TB, args, tail []string, inputs ...[]byte) ([]byte, bool) {
	t.Helper()
	dir := workdir(t, o.dir)
	for i, data := range inputs {
		name := filepath.Join(dir, strconv.Itoa(i))
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
		args = append(args, name)
	}
	args = append(args, tail...)
	cmd := exec.Command(o.binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.Bytes(), false
	}
	if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 && args[0] == "merge" {
		return stdout.Bytes(), true
	}
	t.Fatalf("C reference %s: %v\n%s", args[0], err, stderr.Bytes())
	return nil, false
}

// Diff returns unified hunks without file headers.
func (o *Oracle) Diff(t testing.TB, old, new []byte, opts DiffOptions) []byte {
	t.Helper()
	var tail []string
	for _, anchor := range opts.Anchors {
		tail = append(tail, "--anchor", anchor)
	}
	for _, pattern := range opts.IgnorePatterns {
		tail = append(tail, "--ignore", pattern)
	}
	output, _ := o.runTail(t, []string{"diff", strconv.FormatUint(opts.Flags, 10),
		strconv.Itoa(opts.Context), strconv.Itoa(opts.Interhunk),
		strconv.FormatUint(opts.EmitFlags, 10)}, tail, old, new)
	return output
}

// Script returns compacted changes, before blank-line or regex ignore filters.
func (o *Oracle) Script(t testing.TB, old, new []byte, flags uint64) []Change {
	t.Helper()
	return o.ScriptWithAnchors(t, old, new, flags, nil)
}

// ScriptWithAnchors returns a compacted script with patience anchor prefixes.
func (o *Oracle) ScriptWithAnchors(t testing.TB, old, new []byte, flags uint64, anchors []string) []Change {
	t.Helper()
	var tail []string
	for _, anchor := range anchors {
		tail = append(tail, "--anchor", anchor)
	}
	output, _ := o.runTail(t, []string{"script", strconv.FormatUint(flags, 10)}, tail, old, new)
	var changes []Change
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		var change Change
		if n, err := fmt.Sscanf(line, "%d %d %d %d", &change.OldStart, &change.OldCount,
			&change.NewStart, &change.NewCount); err != nil || n != 4 {
			t.Fatalf("invalid C reference script %q: %v", line, err)
		}
		changes = append(changes, change)
	}
	return changes
}

// Merge returns merged bytes and whether the result contains unresolved conflicts.
func (o *Oracle) Merge(t testing.TB, base, ours, theirs []byte, opts MergeOptions) ([]byte, bool) {
	t.Helper()
	return o.run(t, []string{"merge", strconv.FormatUint(opts.Flags, 10),
		strconv.Itoa(opts.Level), strconv.Itoa(opts.Favor), strconv.Itoa(opts.Style),
		strconv.Itoa(opts.MarkerSize), opts.Ancestor, opts.Ours, opts.Theirs}, base, ours, theirs)
}

// MergeCount returns merged bytes and the exact unresolved conflict count.
func (o *Oracle) MergeCount(t testing.TB, base, ours, theirs []byte, opts MergeOptions) ([]byte, int) {
	t.Helper()
	output, _ := o.run(t, []string{"merge-count", strconv.FormatUint(opts.Flags, 10),
		strconv.Itoa(opts.Level), strconv.Itoa(opts.Favor), strconv.Itoa(opts.Style),
		strconv.Itoa(opts.MarkerSize), opts.Ancestor, opts.Ours, opts.Theirs}, base, ours, theirs)
	header, content, found := bytes.Cut(output, []byte{'\n'})
	count, err := strconv.Atoi(string(header))
	if !found || err != nil || count < 0 {
		t.Fatalf("invalid C merge count %q: %v", header, err)
	}
	return content, count
}
