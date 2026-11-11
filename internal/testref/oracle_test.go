package testref

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestOracle(t *testing.T) {
	if os.Getenv("GO_XDIFF_TESTREF") != "1" {
		t.Skip("set GO_XDIFF_TESTREF=1 to compile and run the C reference")
	}
	oracle := New(t)
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "reference", name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	base, ours, theirs := read("base.txt"), read("ours.txt"), read("theirs.txt")
	if got, want := oracle.Diff(t, base, ours, DiffOptions{Context: 1}), read("diff.txt"); string(got) != string(want) {
		t.Fatalf("diff = %q, want %q", got, want)
	}
	if got := oracle.Script(t, base, ours, 0); !reflect.DeepEqual(got, []Change{{1, 1, 1, 1}}) {
		t.Fatalf("script = %#v", got)
	}
	t.Run("hunk ranges", func(t *testing.T) {
		left := []byte("header\na\nb\nc\nd\ne\nfooter\n")
		right := []byte("header\nA\nb\nc\nd\nE\nfooter\n")
		for _, tc := range []struct {
			name     string
			old, new []byte
			opts     DiffOptions
			want     []Change
		}{
			{"separate", left, right, DiffOptions{}, []Change{{1, 1, 1, 1}, {5, 1, 5, 1}}},
			{"context groups without padding", left, right, DiffOptions{Context: 3}, []Change{{1, 5, 1, 5}}},
			{"interhunk", left, right, DiffOptions{Interhunk: 3}, []Change{{1, 5, 1, 5}}},
			{"output flags do not expand ranges", left, right, DiffOptions{Context: 3, EmitFlags: 7}, []Change{{1, 5, 1, 5}}},
			{"regex filter", left, right, DiffOptions{IgnorePatterns: []string{"^[aA]$"}}, []Change{{5, 1, 5, 1}}},
			{"blank filter", []byte("top\n\nbottom\n"), []byte("top\nbottom\n"), DiffOptions{Flags: 1 << 7}, nil},
			{"unchanged", left, left, DiffOptions{}, nil},
			{"insertion", nil, []byte("new\n"), DiffOptions{Context: 3}, []Change{{0, 0, 0, 1}}},
			{"deletion", []byte("old\n"), nil, DiffOptions{Context: 3}, []Change{{0, 1, 0, 0}}},
			{"anchors", []byte("a\nanchor\nb\n"), []byte("b\nanchor\na\n"),
				DiffOptions{Flags: 1 << 14, Anchors: []string{"anchor"}}, []Change{{0, 1, 0, 1}, {2, 1, 2, 1}}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				if got := oracle.HunkRanges(t, tc.old, tc.new, tc.opts); !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("ranges = %#v, want %#v", got, tc.want)
				}
			})
		}
	})
	for _, flags := range []uint64{0, 1, 1 << 14, 1 << 15, 1 << 23} {
		if got := oracle.Diff(t, base, ours, DiffOptions{Flags: flags, Context: 1}); string(got) != string(read("diff.txt")) {
			t.Fatalf("diff flags %d = %q", flags, got)
		}
	}
	for _, control := range []byte{'\v', '\f'} {
		old := []byte{'a', control, 'b', '\n'}
		want := "@@ -1 +1 @@\n-" + string(old) + "+ab\n"
		if got := oracle.Diff(t, old, []byte("ab\n"), DiffOptions{Flags: 2}); string(got) != want {
			t.Errorf("ignore whitespace with byte %#x = %q, want %q", control, got, want)
		}
	}
	if got := oracle.Diff(t, []byte("old\n"), []byte("new\n"), DiffOptions{
		IgnorePatterns: []string{"^(old|new)$"},
	}); len(got) != 0 {
		t.Fatalf("ignore patterns = %q", got)
	}
	if got := oracle.ScriptWithAnchors(t, []byte("a\nanchor\nb\n"), []byte("b\nanchor\na\n"),
		1<<14, []string{"anchor"}); !reflect.DeepEqual(got, []Change{{0, 1, 0, 1}, {2, 1, 2, 1}}) {
		t.Fatalf("anchored script = %#v", got)
	}
	opts := MergeOptions{Level: 2, Ancestor: "base", Ours: "ours", Theirs: "theirs"}
	got, conflict := oracle.Merge(t, base, ours, theirs, opts)
	if !conflict || string(got) != string(read("merge.txt")) {
		t.Fatalf("merge = %q, conflict = %v", got, conflict)
	}
	if content, count := oracle.MergeCount(t, base, ours, theirs, opts); count != 1 || string(content) != string(got) {
		t.Fatalf("merge count = %d, content = %q", count, content)
	}
	for _, style := range []int{1, 2} {
		styled := opts
		styled.Style, styled.MarkerSize = style, 5
		got, conflict := oracle.Merge(t, base, ours, theirs, styled)
		want := "alpha\n<<<<< ours\nours\n||||| base\nbeta\n=====\ntheirs\n>>>>> theirs\n"
		if !conflict || string(got) != want {
			t.Fatalf("merge style %d = %q, conflict = %v", style, got, conflict)
		}
	}
	opts.Favor = 1
	got, conflict = oracle.Merge(t, base, ours, theirs, opts)
	if conflict || string(got) != string(ours) {
		t.Fatalf("favor ours = %q, conflict = %v", got, conflict)
	}
	got, conflict = oracle.Merge(t, base, base, theirs, MergeOptions{})
	if conflict || string(got) != string(theirs) {
		t.Fatalf("clean merge = %q, conflict = %v", got, conflict)
	}
	if got := oracle.Diff(t, nil, nil, DiffOptions{}); len(got) != 0 {
		t.Fatalf("empty diff = %q", got)
	}
	left, right := []byte("a\nb\nc\nd\ne\n"), []byte("A\nb\nc\nd\nE\n")
	wantJoined := "@@ -1,5 +1,5 @@\n-a\n+A\n b\n c\n d\n-e\n+E\n"
	if got := oracle.Diff(t, left, right, DiffOptions{Interhunk: 3}); string(got) != wantJoined {
		t.Fatalf("interhunk diff = %q", got)
	}
	if got := oracle.Script(t, nil, nil, 0); len(got) != 0 {
		t.Fatalf("empty script = %#v", got)
	}
	if got := oracle.Diff(t, []byte("a"), []byte("b"), DiffOptions{}); string(got) != "@@ -1 +1 @@\n-a\n\\ No newline at end of file\n+b\n\\ No newline at end of file\n" {
		t.Fatalf("unterminated diff = %q", got)
	}
	cmd := exec.Command(oracle.binary, "diff", "0", "0", "0", "0", "missing-input", "missing-input")
	if output, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("missing input succeeded: %s", output)
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 2 {
		t.Fatalf("missing input = %v: %s", err, output)
	}
	t.Logf("diff example:\n%smerge example:\n%s", read("diff.txt"), read("merge.txt"))
}
