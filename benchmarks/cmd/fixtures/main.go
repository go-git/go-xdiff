package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-git/go-xdiff/benchmarks/internal/corpus"
)

func run() error {
	dir := flag.String("dir", "", "output directory for benchmark fixtures")
	flag.Parse()
	if *dir == "" || flag.NArg() != 0 {
		return fmt.Errorf("usage: fixtures -dir DIRECTORY")
	}
	if err := os.MkdirAll(*dir, 0755); err != nil {
		return err
	}
	var manifest []corpus.Manifest
	for _, c := range corpus.Cases() {
		for suffix, data := range map[string][]byte{"old": c.Old, "new": c.New} {
			if err := os.WriteFile(filepath.Join(*dir, c.Name+"."+suffix), data, 0600); err != nil {
				return err
			}
		}
		manifest = append(manifest, c.Manifest())
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(*dir, "manifest.json"), append(data, '\n'), 0600)
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
