package main

import (
	"context"
	"fmt"
	"log"

	"github.com/go-git/go-xdiff"
)

func main() {
	ctx := context.Background()
	old := []byte("alpha\nbeta\ngamma\nalpha\nbeta\ndelta\n")
	new := []byte("alpha\ngamma\nbeta\nalpha\ndelta\nbeta\n")

	for _, spec := range []struct {
		name    string
		options xdiff.Options
	}{
		{"Myers", xdiff.Options{}},
		{"Minimal", xdiff.Options{Minimal: true}},
		{"Patience", xdiff.Options{Algorithm: xdiff.Patience}},
		{"Histogram", xdiff.Options{Algorithm: xdiff.Histogram}},
	} {
		result, err := xdiff.Diff(ctx, old, new, &spec.options)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s:\n", spec.name)
		for _, change := range result.Changes() {
			fmt.Printf("  old[%d:%d] -> new[%d:%d]\n",
				change.OldStart, change.OldStart+change.OldLines,
				change.NewStart, change.NewStart+change.NewLines)
		}
	}
}
