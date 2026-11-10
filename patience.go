// SPDX-License-Identifier: LGPL-2.1-or-later
// Copyright (C) 2003-2016 Davide Libenzi, Johannes E. Schindelin.
// Go translation modified November 9, 2026. See NOTICE.

package xdiff

import (
	"bytes"
	"sort"
)

type patienceEntry struct {
	a, b     int
	previous int
}

func (e *environment) patience() error {
	return e.patienceRange(0, len(e.a.recs), 0, len(e.b.recs))
}

func (e *environment) markRange(f *file, start, count int) error {
	for i := start; i < start+count; i++ {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		f.setChanged(i, true)
	}
	return e.ctx.Err()
}

func (e *environment) patienceRange(ai, an, bi, bn int) error {
	if err := e.ctx.Err(); err != nil {
		return err
	}
	if an == 0 {
		return e.markRange(&e.b, bi, bn)
	}
	if bn == 0 {
		return e.markRange(&e.a, ai, an)
	}

	// Preserve first-file order independently of the hash table.
	entries := make([]patienceEntry, 0, an)
	byID := make(map[int]int, an)
	for i := ai; i < ai+an; i++ {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		id := e.a.recs[i].id
		if j, ok := byID[id]; ok {
			entries[j].b = -2
		} else {
			byID[id] = len(entries)
			entries = append(entries, patienceEntry{a: i, b: -1, previous: -1})
		}
	}
	hasMatches := false
	for i := bi; i < bi+bn; i++ {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		if j, ok := byID[e.b.recs[i].id]; ok {
			hasMatches = true
			if entries[j].b != -1 {
				entries[j].b = -2
			} else {
				entries[j].b = i
			}
		}
	}
	if !hasMatches {
		if err := e.markRange(&e.a, ai, an); err != nil {
			return err
		}
		return e.markRange(&e.b, bi, bn)
	}

	sequence := make([]int, 0, len(entries))
	anchor := -1
	for j := range entries {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		entry := &entries[j]
		if entry.b < 0 {
			continue
		}
		i := sort.Search(len(sequence), func(i int) bool {
			return entries[sequence[i]].b > entry.b
		})
		if i > 0 {
			entry.previous = sequence[i-1]
		}
		if i <= anchor {
			continue
		}
		if i == len(sequence) {
			sequence = append(sequence, j)
		} else {
			sequence[i] = j
		}
		isAnchor, err := e.patienceAnchor(entry.b)
		if err != nil {
			return err
		}
		if isAnchor {
			anchor = i
			sequence = sequence[:i+1]
		}
	}
	if len(sequence) == 0 {
		return fallbackDiff(e.ctx, &e.a, ai, an, &e.b, bi, bn, e.opts)
	}

	common := make([]int, len(sequence))
	for i, j := len(common)-1, sequence[len(sequence)-1]; i >= 0; i-- {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		common[i] = j
		j = entries[j].previous
	}
	endA, endB := ai+an, bi+bn
	for pos := 0; ; pos++ {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		nextA, nextB := endA, endB
		if pos < len(common) {
			entry := entries[common[pos]]
			nextA, nextB = entry.a, entry.b
			for nextA > ai && nextB > bi && e.a.recs[nextA-1].id == e.b.recs[nextB-1].id {
				if err := e.ctx.Err(); err != nil {
					return err
				}
				nextA--
				nextB--
			}
		}
		for ai < nextA && bi < nextB && e.a.recs[ai].id == e.b.recs[bi].id {
			if err := e.ctx.Err(); err != nil {
				return err
			}
			ai++
			bi++
		}
		if nextA > ai || nextB > bi {
			if err := e.patienceRange(ai, nextA-ai, bi, nextB-bi); err != nil {
				return err
			}
		}
		if pos == len(common) {
			return e.ctx.Err()
		}
		for pos+1 < len(common) {
			current, next := entries[common[pos]], entries[common[pos+1]]
			if next.a != current.a+1 || next.b != current.b+1 {
				break
			}
			if err := e.ctx.Err(); err != nil {
				return err
			}
			pos++
		}
		last := entries[common[pos]]
		ai, bi = last.a+1, last.b+1
	}
}

func (e *environment) patienceAnchor(line int) (bool, error) {
	// C compares a prefix at the record pointer, not a bounded line.
	data := e.b.data[e.b.recs[line].start:]
	for _, prefix := range e.opts.Anchors {
		if err := e.ctx.Err(); err != nil {
			return false, err
		}
		if i := bytes.IndexByte([]byte(prefix), 0); i >= 0 {
			prefix = prefix[:i]
		}
		if bytes.HasPrefix(data, []byte(prefix)) {
			return true, nil
		}
	}
	return false, nil
}
