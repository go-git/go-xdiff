// SPDX-License-Identifier: LGPL-2.1-or-later
// Copyright (C) 2003 Davide Libenzi.
// Go translation modified November 2026. See NOTICE.

package xdiff

import (
	"context"
	"errors"
	"fmt"
	"slices"
)

// Result contains a diff and references to its input bytes.
// Its methods do not change the result.
type Result struct {
	a, b    file
	changes []Change
	opts    Options
}

func validateOptions(opts Options) error {
	if opts.Algorithm > Histogram {
		return fmt.Errorf("xdiff: invalid algorithm %d", opts.Algorithm)
	}
	if opts.Minimal && opts.Algorithm != Myers {
		return errors.New("xdiff: minimal mode requires Myers")
	}
	if opts.Whitespace & ^(IgnoreAllSpace|IgnoreSpaceChange|IgnoreSpaceAtEOL|IgnoreCRAtEOL) != 0 {
		return errors.New("xdiff: invalid whitespace flags")
	}
	for _, expression := range opts.IgnoreRegex {
		if expression == nil {
			return errors.New("xdiff: nil ignore expression")
		}
	}
	return nil
}

// Diff computes line replacements from old to new. It does not modify inputs
// or options. Nil options select Myers without the indent heuristic.
func Diff(ctx context.Context, old, new []byte, options *Options) (*Result, error) {
	if ctx == nil {
		return nil, errors.New("xdiff: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opts := Options{}
	if options != nil {
		opts = *options
		// Results retain options for output filters, not caller-owned slices.
		opts.IgnoreRegex = slices.Clone(opts.IgnoreRegex)
		opts.Anchors = slices.Clone(opts.Anchors)
	}
	if err := validateOptions(opts); err != nil {
		return nil, err
	}
	e, err := prepare(ctx, old, new, opts)
	if err != nil {
		return nil, err
	}
	switch opts.Algorithm {
	case Myers:
		err = e.myers()
	case Patience:
		err = e.patience()
	case Histogram:
		err = e.histogram()
	}
	if err != nil {
		return nil, err
	}
	if err := e.compact(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Result{a: e.a, b: e.b, changes: e.script(), opts: opts}, nil
}

// Changes returns an independent copy of the compacted edit script.
// Output suppression does not remove changes from this script.
func (r *Result) Changes() []Change {
	return slices.Clone(r.changes)
}

// Chunks returns equal, delete, and insert chunks in source order.
// Each chunk refers to the original input bytes.
func (r *Result) Chunks() []Chunk {
	var chunks []Chunk
	a, b := 0, 0
	for _, c := range r.changes {
		if c.OldStart > a || c.NewStart > b {
			chunks = append(chunks, Chunk{Equal,
				r.a.region(a, c.OldStart-a), r.b.region(b, c.NewStart-b)})
		}
		if c.OldLines > 0 {
			chunks = append(chunks, Chunk{Operation: Delete, Old: r.a.region(c.OldStart, c.OldLines)})
		}
		if c.NewLines > 0 {
			chunks = append(chunks, Chunk{Operation: Insert, New: r.b.region(c.NewStart, c.NewLines)})
		}
		a, b = c.OldStart+c.OldLines, c.NewStart+c.NewLines
	}
	if a < len(r.a.recs) || b < len(r.b.recs) {
		chunks = append(chunks, Chunk{Equal, r.a.region(a, len(r.a.recs)-a), r.b.region(b, len(r.b.recs)-b)})
	}
	return chunks
}
