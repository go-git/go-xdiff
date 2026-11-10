package main

import (
	"context"
	"fmt"
	"log"

	"github.com/go-git/go-xdiff"
)

func main() {
	ctx := context.Background()
	base := []byte("name: demo\ncolor: blue\nshape: circle\nsize: small\n")
	ours := []byte("name: demo\ncolor: green\nshape: circle\nsize: small\n")
	theirs := []byte("name: demo\ncolor: blue\nshape: circle\nsize: large\n")

	result, err := xdiff.Merge(ctx, base, ours, theirs, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Clean merge (%d conflicts):\n%s", result.Conflicts, result.Content)

	theirs = []byte("name: demo\ncolor: red\nshape: circle\nsize: small\n")
	result, err = xdiff.Merge(ctx, base, ours, theirs, &xdiff.MergeOptions{
		Level:         xdiff.MergeZealous,
		Style:         xdiff.ConflictDiff3,
		AncestorLabel: "base",
		OursLabel:     "ours",
		TheirsLabel:   "theirs",
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nConflicted merge (%d conflicts):\n%s", result.Conflicts, result.Content)
}
