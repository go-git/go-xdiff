package xdiff_test

import (
	"bytes"
	"context"
	"fmt"

	"github.com/go-git/go-xdiff"
)

func ExampleResult_Emit() {
	result, err := xdiff.Diff(context.Background(), []byte("old"), []byte("new"), nil)
	if err != nil {
		panic(err)
	}
	var output bytes.Buffer
	err = result.Emit(context.Background(), nil, &xdiff.EmitCallbacks{
		HunkHeaderFunc: func(h xdiff.Hunk) error {
			_, err := fmt.Fprintf(&output, "old=%d new=%d\n", h.OldLines, h.NewLines)
			return err
		},
		LineFunc: func(parts ...[]byte) error {
			for _, part := range parts {
				output.Write(part)
			}
			return nil
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Print(output.String())
	// Output:
	// old=1 new=1
	// -old
	// \ No newline at end of file
	// +new
	// \ No newline at end of file
}

func ExampleResult_Emit_colors() {
	result, err := xdiff.Diff(context.Background(), []byte("old\n"), []byte("new\n"), nil)
	if err != nil {
		panic(err)
	}
	var output bytes.Buffer
	err = result.Emit(context.Background(), nil, &xdiff.EmitCallbacks{
		LineFunc: func(parts ...[]byte) error {
			color := "\x1b[36m"
			switch parts[0][0] {
			case '-':
				color = "\x1b[31m"
			case '+':
				color = "\x1b[32m"
			}
			output.WriteString(color)
			for _, part := range parts {
				output.Write(part)
			}
			output.WriteString("\x1b[0m")
			return nil
		},
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("%q\n", output.String())
	// Output:
	// "\x1b[36m@@ -1 +1 @@\n\x1b[0m\x1b[31m-old\n\x1b[0m\x1b[32m+new\n\x1b[0m"
}

func ExampleDiff_minimal() {
	result, err := xdiff.Diff(context.Background(), []byte("a\nb\n"), []byte("b\na\n"),
		&xdiff.Options{Minimal: true})
	if err != nil {
		panic(err)
	}
	deleted, inserted := 0, 0
	for _, change := range result.Changes() {
		deleted += change.OldLines
		inserted += change.NewLines
	}
	fmt.Println(deleted, "deleted,", inserted, "inserted")
	// Output:
	// 1 deleted, 1 inserted
}

func ExampleDiff_whitespace() {
	for _, mode := range []xdiff.Whitespace{
		xdiff.IgnoreAllSpace, xdiff.IgnoreSpaceChange, xdiff.IgnoreSpaceAtEOL, xdiff.IgnoreCRAtEOL,
	} {
		result, err := xdiff.Diff(context.Background(), []byte("line\r\n"), []byte("line\n"),
			&xdiff.Options{Whitespace: mode})
		if err != nil {
			panic(err)
		}
		fmt.Println(len(result.Changes()))
	}
	// Output:
	// 0
	// 0
	// 0
	// 0
}

func ExampleResult_Chunks() {
	result, err := xdiff.Diff(context.Background(), []byte("one\ntwo\n"), []byte("one\nthree\n"), nil)
	if err != nil {
		panic(err)
	}
	for _, chunk := range result.Chunks() {
		// A patch adapter uses destination bytes for unchanged context.
		content := chunk.New
		if chunk.Operation == xdiff.Delete {
			content = chunk.Old
		}
		fmt.Printf("%d %q\n", chunk.Operation, content)
	}
	// Output:
	// 0 "one\n"
	// -1 "two\n"
	// 1 "three\n"
}
