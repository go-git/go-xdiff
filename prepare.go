// SPDX-License-Identifier: LGPL-2.1-or-later
// Copyright (C) 2003 Davide Libenzi.
// Go translation modified November 2026. See NOTICE.

package xdiff

import "context"

type environment struct {
	a, b             file
	opts             Options
	ctx              context.Context
	histogramScratch histogramIndex
}

func prepare(ctx context.Context, a, b []byte, opts Options) (*environment, error) {
	var classes classifier
	left, err := readFile(ctx, a, opts.Whitespace, &classes)
	if err != nil {
		return nil, err
	}
	right, err := readFile(ctx, b, opts.Whitespace, &classes)
	if err != nil {
		return nil, err
	}
	e := &environment{a: left, b: right, opts: opts, ctx: ctx}
	if opts.Algorithm == Myers {
		e.trim()
		if err := e.cleanup(len(classes.entries)); err != nil {
			return nil, err
		}
	}
	return e, nil
}

func (e *environment) trim() {
	a, b := &e.a, &e.b
	n := min(len(a.recs), len(b.recs))
	start := 0
	for start < n && a.recs[start].id == b.recs[start].id {
		start++
	}
	a.start, b.start = start, start
	end := 0
	for end < n-start && a.recs[len(a.recs)-1-end].id == b.recs[len(b.recs)-1-end].id {
		end++
	}
	a.end, b.end = len(a.recs)-end-1, len(b.recs)-end-1
}

func cleanMultimatch(action []uint8, i int) bool {
	// Slice the scan windows once to remove per-record bounds checks.
	before := action[max(0, i-100):i]
	beforeDiscard := 0
	j := len(before) - 1
	for ; j >= 0 && before[j] != 1; j-- {
		if before[j] == 0 {
			beforeDiscard++
		}
	}
	if beforeDiscard == 0 {
		return false
	}
	// Each scanned record is either a discard or a multimatch.
	beforeMulti := len(before) - j - beforeDiscard
	after := action[i+1 : min(len(action), i+101)]
	afterDiscard := 0
	j = 0
	for ; j < len(after) && after[j] != 1; j++ {
		if after[j] == 0 {
			afterDiscard++
		}
	}
	if afterDiscard == 0 {
		return false
	}
	afterMulti := j + 1 - afterDiscard
	multi := beforeMulti + afterMulti
	return multi*4 < multi+beforeDiscard+afterDiscard
}

func (e *environment) cleanup(classCount int) error {
	if e.a.start > e.a.end && e.b.start > e.b.end {
		return nil
	}
	counts := make([][2]int, classCount)
	for _, r := range e.a.recs {
		counts[r.id][0]++
	}
	for _, r := range e.b.recs {
		counts[r.id][1]++
	}
	for pass := 0; pass < 2; pass++ {
		f := &e.a
		if pass == 1 {
			f = &e.b
		}
		limit := min(bogosqrt(len(f.recs)), 1024)
		if e.opts.Minimal {
			limit = int(^uint(0) >> 1)
		}
		actions := make([]uint8, f.end-f.start+1)
		references := 0
		for i := range actions {
			n := counts[f.recs[i+f.start].id][1-pass]
			if n > 0 {
				references++
				actions[i] = 1
				if n >= limit {
					actions[i] = 2
				}
			}
		}
		f.refs = make([]int, 0, references)
		for i, action := range actions {
			if err := e.ctx.Err(); err != nil {
				return err
			}
			if action == 1 || action == 2 && !cleanMultimatch(actions, i) {
				f.refs = append(f.refs, i+f.start)
			} else {
				f.setChanged(i+f.start, true)
			}
		}
	}
	return nil
}

func fallbackDiff(ctx context.Context, a *file, ai, an int, b *file, bi, bn int, opts Options) error {
	opts.Algorithm, opts.Minimal, opts.Anchors = Myers, false, nil
	e := environment{ctx: ctx, opts: opts}
	classes := make(map[int]int)
	for pass, source := range []*file{a, b} {
		start, count, target := ai, an, &e.a
		if pass == 1 {
			start, count, target = bi, bn, &e.b
		}
		target.data = source.region(start, count)
		target.recs = make([]record, count)
		target.changed = make([]bool, count+2)
		target.end = count - 1
		offset := len(source.data)
		if start < len(source.recs) {
			offset = source.recs[start].start
		}
		for i, r := range source.recs[start : start+count] {
			if err := ctx.Err(); err != nil {
				return err
			}
			id, found := classes[r.id]
			if !found {
				id = len(classes)
				classes[r.id] = id
			}
			target.recs[i] = record{id: id, start: r.start - offset}
		}
	}
	e.trim()
	if err := e.cleanup(len(classes)); err != nil {
		return err
	}
	if err := e.myers(); err != nil {
		return err
	}
	for i := 0; i < an; i++ {
		a.setChanged(ai+i, e.a.isChanged(i))
	}
	for i := 0; i < bn; i++ {
		b.setChanged(bi+i, e.b.isChanged(i))
	}
	return nil
}
