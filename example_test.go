package xdiff_test

import (
	"context"
	"fmt"

	"github.com/go-git/go-xdiff"
)

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
