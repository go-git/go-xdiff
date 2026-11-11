// SPDX-License-Identifier: LGPL-2.1-or-later
// Copyright (C) 2003 Davide Libenzi.
// Go translation modified November 2026. See NOTICE.

package xdiff

import (
	"context"
	"errors"
)

type emitChange struct {
	Change
	ignore bool
}

func saturatingAdd(a, b int) int {
	const maxInt = int(^uint(0) >> 1)
	if a > maxInt-b {
		return maxInt
	}
	return a + b
}

func emitOptions(ctx context.Context, options *EmitOptions) (EmitOptions, error) {
	if ctx == nil {
		return EmitOptions{}, errors.New("xdiff: nil context")
	}
	if err := ctx.Err(); err != nil {
		return EmitOptions{}, err
	}
	opts := EmitOptions{}
	if options != nil {
		opts = *options
	}
	if opts.Context < 0 || opts.InterHunkContext < 0 {
		return EmitOptions{}, errors.New("xdiff: negative context length")
	}
	return opts, nil
}

func (r *Result) emitChanges(ctx context.Context) ([]emitChange, error) {
	changes := make([]emitChange, len(r.changes))
	for i, change := range r.changes {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		changes[i].Change = change
		allBlank, allMatch := r.opts.IgnoreBlankLines, len(r.opts.IgnoreRegex) > 0
		for pass, f := range []*file{&r.a, &r.b} {
			start, count := change.OldStart, change.OldLines
			if pass == 1 {
				start, count = change.NewStart, change.NewLines
			}
			for j := start; j < start+count && (allBlank || allMatch); j++ {
				line := f.region(j, 1)
				if err := ctx.Err(); err != nil {
					return nil, err
				}
				if allBlank {
					if r.opts.Whitespace == 0 {
						allBlank = len(line) <= 1
					} else {
						for _, c := range line {
							if !isSpace(c) {
								allBlank = false
								break
							}
						}
					}
				}
				if allMatch {
					matched := false
					for _, expression := range r.opts.IgnoreRegex {
						if expression.Match(line) {
							matched = true
							break
						}
					}
					allMatch = matched
				}
			}
		}
		changes[i].ignore = allBlank || allMatch
	}
	return changes, nil
}

func getHunk(ctx context.Context, changes []emitChange, start int, opts EmitOptions) (int, int, error) {
	maxCommon := saturatingAdd(saturatingAdd(opts.Context, opts.Context), opts.InterHunkContext)
	for p := start; p < len(changes) && changes[p].ignore; p++ {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		if p+1 == len(changes) || changes[p+1].OldStart-(changes[p].OldStart+changes[p].OldLines) >= opts.Context {
			start = p + 1
		}
	}
	if start == len(changes) {
		return start, start, nil
	}
	last, ignored := start, 0
	for p, i := start, start+1; i < len(changes); p, i = i, i+1 {
		if err := ctx.Err(); err != nil {
			return 0, 0, err
		}
		c, prev := changes[i], changes[p]
		distance := c.OldStart - (prev.OldStart + prev.OldLines)
		switch {
		case distance > maxCommon:
			return start, last, nil
		case distance < opts.Context && (!c.ignore || last == p):
			last, ignored = i, 0
		case distance < opts.Context && c.ignore:
			ignored = saturatingAdd(ignored, c.NewLines)
		case last != p && saturatingAdd(c.OldStart-(changes[last].OldStart+changes[last].OldLines), ignored) > maxCommon:
			return start, last, nil
		case !c.ignore:
			last, ignored = i, 0
		default:
			ignored = saturatingAdd(ignored, c.NewLines)
		}
	}
	return start, last, nil
}

type hunkBuilder struct {
	ctx  context.Context
	r    *Result
	opts EmitOptions
	err  error
}

func (b *hunkBuilder) check() bool {
	if b.err == nil {
		b.err = b.ctx.Err()
	}
	return b.err == nil
}

func (b *hunkBuilder) function(line []byte, size int) (string, bool) {
	if !b.check() {
		return "", false
	}
	if b.opts.FindFunc != nil {
		name, ok := b.opts.FindFunc(line)
		if len(name) > size {
			name = name[:size]
		}
		return name, ok
	}
	if len(line) == 0 || ((line[0] < 'a' || line[0] > 'z') &&
		(line[0] < 'A' || line[0] > 'Z') && line[0] != '_' && line[0] != '$') {
		return "", false
	}
	line = line[:min(len(line), size)]
	for len(line) > 0 && isSpace(line[len(line)-1]) {
		line = line[:len(line)-1]
	}
	return string(line), true
}

func (b *hunkBuilder) findFunction(start, limit int, name *string) int {
	step, size := 1, 1
	if start > limit {
		step = -1
	}
	if name != nil {
		size = 80
	}
	for i := start; i != limit && i >= 0 && i < len(b.r.a.recs) && b.check(); i += step {
		if value, ok := b.function(b.r.a.region(i, 1), size); ok {
			if name != nil {
				*name = value
			}
			return i
		}
	}
	return -1
}

func (b *hunkBuilder) empty(i int) bool {
	for _, c := range b.r.a.region(i, 1) {
		if !isSpace(c) {
			return false
		}
	}
	return true
}

// Hunks returns unified hunks whose line contents refer to the original inputs.
// NoHunkHeader affects Emit and WriteUnified only.
func (r *Result) Hunks(ctx context.Context, options *EmitOptions) ([]Hunk, error) {
	opts, err := emitOptions(ctx, options)
	if err != nil {
		return nil, err
	}
	var hunks []Hunk
	err = r.eachHunk(ctx, opts, func(h Hunk) error {
		hunks = append(hunks, h)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hunks, nil
}

func (r *Result) eachHunk(ctx context.Context, opts EmitOptions, emit func(Hunk) error) error {
	changes, err := r.emitChanges(ctx)
	if err != nil {
		return err
	}
	b := hunkBuilder{ctx: ctx, r: r, opts: opts}
	previousFunction, function := -1, ""
	for next := 0; next < len(changes) && b.check(); {
		start, last, err := getHunk(ctx, changes, next, opts)
		if err != nil {
			return err
		}
		if start == len(changes) {
			break
		}
		var s1, s2, e1, e2 int
		p := next
		for b.check() {
			first := changes[start]
			s1, s2 = max(first.OldStart-opts.Context, 0), max(first.NewStart-opts.Context, 0)
			if !opts.FunctionContext {
				break
			}
			i1 := first.OldStart
			if i1 >= len(r.a.recs) {
				wholeFunction := false
				for i2 := first.NewStart; i2 < len(r.b.recs) && b.check(); i2++ {
					if _, ok := b.function(r.b.region(i2, 1), 1); ok {
						wholeFunction = true
						break
					}
				}
				if wholeFunction {
					break
				}
				i1 = len(r.a.recs) - 1
			}
			fs1 := b.findFunction(i1, -1, nil)
			for fs1 > 0 && b.check() && !b.empty(fs1-1) {
				if _, ok := b.function(r.a.region(fs1-1, 1), 1); ok {
					break
				}
				fs1--
			}
			fs1 = max(fs1, 0)
			if fs1 >= s1 {
				break
			}
			s2, s1 = max(s2-(s1-fs1), 0), fs1
			for p != start && b.check() &&
				changes[p].OldStart+changes[p].OldLines <= s1 &&
				changes[p].NewStart+changes[p].NewLines <= s2 {
				p++
			}
			if p == start {
				break
			}
			start = p
		}
		for b.check() {
			end := changes[last]
			lctx := min(opts.Context, len(r.a.recs)-(end.OldStart+end.OldLines),
				len(r.b.recs)-(end.NewStart+end.NewLines))
			e1, e2 = end.OldStart+end.OldLines+lctx, end.NewStart+end.NewLines+lctx
			if !opts.FunctionContext {
				break
			}
			fe1 := b.findFunction(end.OldStart+end.OldLines, len(r.a.recs), nil)
			for fe1 > 0 && b.check() && b.empty(fe1-1) {
				fe1--
			}
			if fe1 < 0 {
				fe1 = len(r.a.recs)
			}
			if fe1 > e1 {
				e2, e1 = e2+min(fe1-e1, len(r.b.recs)-e2), fe1
			}
			if last+1 == len(changes) {
				break
			}
			l := min(changes[last+1].OldStart, len(r.a.recs)-1)
			if l-opts.Context > e1 && b.findFunction(l, e1, nil) >= 0 {
				break
			}
			last++
		}
		if opts.FunctionNames {
			b.findFunction(s1-1, previousFunction, &function)
			previousFunction = s1 - 1
		}
		lineCount := e2 - s2
		for i := start; i <= last; i++ {
			if !b.check() {
				return b.err
			}
			lineCount += changes[i].OldLines
		}
		h := Hunk{Change: Change{s1, e1 - s1, s2, e2 - s2}, Function: function,
			Lines: make([]Line, 0, lineCount)}
		add := func(f *file, from, to int, op Operation) {
			for i := from; i < to && b.check(); i++ {
				h.Lines = append(h.Lines, Line{op, f.region(i, 1)})
			}
		}
		add(&r.b, s2, changes[start].NewStart, Equal)
		a, n := changes[start].OldStart, changes[start].NewStart
		for i := start; i <= last && b.check(); i++ {
			c := changes[i]
			add(&r.b, n, n+min(c.OldStart-a, c.NewStart-n), Equal)
			add(&r.a, c.OldStart, c.OldStart+c.OldLines, Delete)
			add(&r.b, c.NewStart, c.NewStart+c.NewLines, Insert)
			a, n = c.OldStart+c.OldLines, c.NewStart+c.NewLines
		}
		add(&r.b, n, e2, Equal)
		if !b.check() {
			return b.err
		}
		if err := emit(h); err != nil {
			return err
		}
		next = last + 1
	}
	if !b.check() {
		return b.err
	}
	return nil
}
