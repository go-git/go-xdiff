/*
 * Copyright (C) 2010, Google Inc.
 * and other copyright owners as documented in JGit's IP log.
 *
 * This program and the accompanying materials are made available
 * under the terms of the Eclipse Distribution License v1.0 which
 * accompanies this distribution, is reproduced below, and is
 * available at http://www.eclipse.org/org/documents/edl-v10.php
 *
 * All rights reserved.
 *
 * Redistribution and use in source and binary forms, with or
 * without modification, are permitted provided that the following
 * conditions are met:
 *
 * - Redistributions of source code must retain the above copyright
 *   notice, this list of conditions and the following disclaimer.
 *
 * - Redistributions in binary form must reproduce the above
 *   copyright notice, this list of conditions and the following
 *   disclaimer in the documentation and/or other materials provided
 *   with the distribution.
 *
 * - Neither the name of the Eclipse Foundation, Inc. nor the
 *   names of its contributors may be used to endorse or promote
 *   products derived from this software without specific prior
 *   written permission.
 *
 * THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND
 * CONTRIBUTORS "AS IS" AND ANY EXPRESS OR IMPLIED WARRANTIES,
 * INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES
 * OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
 * ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT OWNER OR
 * CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
 * SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT
 * NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
 * LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
 * CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT,
 * STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
 * ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF
 * ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
 *
 * Go translation modified November 9, 2026.
 */

package xdiff

import (
	"fmt"
	"math/bits"
)

const histogramMaxChain = 64

type histogramRecord struct {
	ptr, count int
	next       *histogramRecord
}

type histogramIndex struct {
	buckets []*histogramRecord
	lines   []*histogramRecord
	next    []int
	shift   int
	bits    int
	count   int
	common  bool
}

type histogramRegion struct {
	a, ae, b, be int
	found        bool
}

func (h *histogramIndex) hash(id int) int {
	// XDL_HASHLONG folds the upper bits into the bucket index.
	return (id + (id >> h.bits)) & (len(h.buckets) - 1)
}

func (e *environment) histogram() error {
	return e.histogramRange(e.a.start, e.a.end-e.a.start+1,
		e.b.start, e.b.end-e.b.start+1)
}

func (e *environment) histogramRange(ai, an, bi, bn int) error {
	for {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		if an == 0 {
			return e.markRange(&e.b, bi, bn)
		}
		if bn == 0 {
			return e.markRange(&e.a, ai, an)
		}
		lcs, fallback, err := e.histogramLCS(ai, an, bi, bn)
		if err != nil {
			return err
		}
		if fallback {
			return fallbackDiff(e.ctx, &e.a, ai, an, &e.b, bi, bn, e.opts)
		}
		if !lcs.found {
			if err := e.markRange(&e.a, ai, an); err != nil {
				return err
			}
			return e.markRange(&e.b, bi, bn)
		}
		if err := e.histogramRange(ai, lcs.a-ai, bi, lcs.b-bi); err != nil {
			return err
		}
		an, bn = ai+an-lcs.ae-1, bi+bn-lcs.be-1
		ai, bi = lcs.ae+1, lcs.be+1
	}
}

func (e *environment) histogramLCS(ai, an, bi, bn int) (histogramRegion, bool, error) {
	var lcs histogramRegion
	h := histogramIndex{
		lines: make([]*histogramRecord, an),
		next:  make([]int, an), shift: ai,
		bits: max(1, bits.Len(uint(an-1))), count: histogramMaxChain + 1,
	}
	h.buckets = make([]*histogramRecord, 1<<h.bits)
	records := make([]histogramRecord, an)
	for ptr := ai + an - 1; ptr >= ai; ptr-- {
		if err := e.ctx.Err(); err != nil {
			return lcs, false, err
		}
		h.next[ptr-ai] = -1
		bucket := h.hash(e.a.recs[ptr].id)
		rec := h.buckets[bucket]
		chain := 0
		for rec != nil && e.a.recs[rec.ptr].id != e.a.recs[ptr].id {
			if err := e.ctx.Err(); err != nil {
				return lcs, false, err
			}
			rec = rec.next
			chain++
		}
		if rec != nil {
			h.next[ptr-ai] = rec.ptr
			rec.ptr = ptr
			rec.count++
		} else {
			if chain == histogramMaxChain {
				return lcs, false, fmt.Errorf("xdiff: histogram hash chain exceeds %d records", histogramMaxChain)
			}
			rec = &records[ptr-ai]
			*rec = histogramRecord{ptr: ptr, count: 1, next: h.buckets[bucket]}
			h.buckets[bucket] = rec
		}
		h.lines[ptr-ai] = rec
	}
	for ptr := bi; ptr < bi+bn; {
		if err := e.ctx.Err(); err != nil {
			return lcs, false, err
		}
		next, err := e.histogramTry(&h, &lcs, ptr, ai, an, bi, bn)
		if err != nil {
			return lcs, false, err
		}
		ptr = next
	}
	return lcs, h.common && h.count > histogramMaxChain, nil
}

func (e *environment) histogramTry(h *histogramIndex, lcs *histogramRegion, ptr, ai, an, bi, bn int) (int, error) {
	nextB := ptr + 1
	for rec := h.buckets[h.hash(e.b.recs[ptr].id)]; rec != nil; rec = rec.next {
		if err := e.ctx.Err(); err != nil {
			return 0, err
		}
		matches := e.a.recs[rec.ptr].id == e.b.recs[ptr].id
		if rec.count > h.count {
			h.common = h.common || matches
			continue
		}
		if !matches {
			continue
		}
		h.common = true
		for as := rec.ptr; ; {
			if err := e.ctx.Err(); err != nil {
				return 0, err
			}
			next := h.next[as-h.shift]
			ae, bs, be, rarity := as, ptr, ptr, rec.count
			for ai < as && bi < bs && e.a.recs[as-1].id == e.b.recs[bs-1].id {
				if err := e.ctx.Err(); err != nil {
					return 0, err
				}
				as--
				bs--
				if rarity > 1 {
					rarity = min(rarity, h.lines[as-h.shift].count)
				}
			}
			for ae+1 < ai+an && be+1 < bi+bn && e.a.recs[ae+1].id == e.b.recs[be+1].id {
				if err := e.ctx.Err(); err != nil {
					return 0, err
				}
				ae++
				be++
				if rarity > 1 {
					rarity = min(rarity, h.lines[ae-h.shift].count)
				}
			}
			nextB = max(nextB, be+1)
			if lcs.ae-lcs.a < ae-as || rarity < h.count {
				*lcs = histogramRegion{a: as, ae: ae, b: bs, be: be, found: true}
				h.count = rarity
			}
			for next >= 0 && next <= ae {
				if err := e.ctx.Err(); err != nil {
					return 0, err
				}
				next = h.next[next-h.shift]
			}
			if next < 0 {
				break
			}
			as = next
		}
	}
	return nextB, nil
}
