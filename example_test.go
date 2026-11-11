package xdiff_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/go-git/go-xdiff"
)

func ExampleResult_Emit() {
	result, err := xdiff.Diff(context.Background(), []byte("old\n"), []byte("new\n"), nil)
	if err != nil {
		panic(err)
	}
	var output bytes.Buffer
	err = result.Emit(context.Background(), nil, &xdiff.EmitCallbacks{
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
	// @@ -1 +1 @@
	// -old
	// +new
}

func ExampleResult_Emit_customHeader() {
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

func ExampleResult_Emit_noHeader() {
	result, err := xdiff.Diff(context.Background(), []byte("old\n"), []byte("new\n"), nil)
	if err != nil {
		panic(err)
	}
	var output bytes.Buffer
	err = result.Emit(context.Background(), &xdiff.EmitOptions{NoHunkHeader: true}, &xdiff.EmitCallbacks{
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
	// -old
	// +new
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

func ExampleResult_Emit_ranges() {
	result, err := xdiff.Diff(context.Background(),
		[]byte("head\nold-one\nsame\nold-two\ntail\n"),
		[]byte("head\nnew-one\nsame\nnew-two\ntail\n"), nil)
	if err != nil {
		panic(err)
	}
	err = result.Emit(context.Background(), &xdiff.EmitOptions{
		Context: 1,
		HunkFunc: func(change xdiff.Change) error {
			_, err := fmt.Printf("old=[%d,%d) new=[%d,%d)\n",
				change.OldStart, change.OldStart+change.OldLines,
				change.NewStart, change.NewStart+change.NewLines)
			return err
		},
	}, nil)
	if err != nil {
		panic(err)
	}
	// Output: old=[1,4) new=[1,4)
}

func ExampleResult_Emit_stop() {
	result, err := xdiff.Diff(context.Background(), []byte("old\nsame\nold\n"), []byte("new\nsame\nnew\n"), nil)
	if err != nil {
		panic(err)
	}
	stop := errors.New("stop")
	calls := 0
	err = result.Emit(context.Background(), &xdiff.EmitOptions{
		HunkFunc: func(xdiff.Change) error {
			calls++
			return stop
		},
	}, nil)
	fmt.Println("calls:", calls)
	fmt.Println("stopped:", errors.Is(err, stop))
	// Output:
	// calls: 1
	// stopped: true
}

func ExampleResult_HunkRanges() {
	result, err := xdiff.Diff(context.Background(),
		[]byte("head\nold-one\nsame\nold-two\ntail\n"),
		[]byte("head\nnew-one\nsame\nnew-two\ntail\n"), nil)
	if err != nil {
		panic(err)
	}
	ranges, err := result.HunkRanges(context.Background(), &xdiff.EmitOptions{Context: 1})
	if err != nil {
		panic(err)
	}
	fmt.Println(ranges)
	// Output: [{1 3 1 3}]
}

func ExampleResult_HunkRanges_filters() {
	result, err := xdiff.Diff(context.Background(),
		[]byte("head\n\nkeep\ntimestamp: old\nsame\nold\n"),
		[]byte("head\nkeep\ntimestamp: new\nsame\nnew\n"),
		&xdiff.Options{IgnoreBlankLines: true,
			IgnoreRegex: []*regexp.Regexp{regexp.MustCompile("^timestamp:")}})
	if err != nil {
		panic(err)
	}
	ranges, err := result.HunkRanges(context.Background(), nil)
	if err != nil {
		panic(err)
	}
	fmt.Println("raw changes:", len(result.Changes()))
	fmt.Println("ranges:", ranges)
	// Output:
	// raw changes: 3
	// ranges: [{5 1 4 1}]
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
