package main

import (
	"bufio"
	"context"
	"log"
	"os"

	"github.com/go-git/go-xdiff"
)

func main() {
	ctx := context.Background()
	old := []byte("one\ntwo\nthree\n")
	new := []byte("one\nTWO\nthree\nfour\n")

	result, err := xdiff.Diff(ctx, old, new, nil)
	if err != nil {
		log.Fatal(err)
	}
	output := bufio.NewWriter(os.Stdout)
	if err := xdiff.WriteUnified(ctx, output, result, &xdiff.EmitOptions{Context: 1}); err != nil {
		log.Fatal(err)
	}
	if err := output.Flush(); err != nil {
		log.Fatal(err)
	}
}
