// SPDX-License-Identifier: LGPL-2.1-or-later
// Copyright (C) 2003 Davide Libenzi
// Go translation modified November 2026.
// Port of xdiff/xdiffi.c.

package xdiff

import "fmt"

const (
	myersMaxCostMin = 256
	myersHeurMin    = 256
	myersSnake      = 20
	myersKHeur      = 4
)

type myersSplit struct {
	a, b         int
	minLo, minHi bool
}

type myersState struct {
	e        *environment
	forward  []int
	backward []int
	base     int
	maxCost  int
}

func validateChanges(f *file) error {
	if len(f.changed) != len(f.recs)+2 {
		return fmt.Errorf("xdiff: invalid change array length")
	}
	if f.changed[0] || f.changed[len(f.changed)-1] {
		return fmt.Errorf("xdiff: nonzero change sentinel")
	}
	return nil
}

func (e *environment) myers() error {
	if err := e.ctx.Err(); err != nil {
		return err
	}
	for _, f := range []*file{&e.a, &e.b} {
		if err := validateChanges(f); err != nil {
			return err
		}
		for i, ref := range f.refs {
			if i&1023 == 0 {
				if err := e.ctx.Err(); err != nil {
					return err
				}
			}
			if ref < 0 || ref >= len(f.recs) {
				return fmt.Errorf("xdiff: invalid reference index %d", ref)
			}
		}
	}
	ndiags := len(e.a.refs) + len(e.b.refs) + 3
	s := myersState{
		e: e, forward: make([]int, ndiags), backward: make([]int, ndiags),
		base: len(e.b.refs) + 1, maxCost: max(myersMaxCostMin, bogosqrt(ndiags)),
	}
	return s.compare(0, len(e.a.refs), 0, len(e.b.refs), e.opts.Minimal)
}

func (s *myersState) equal(a, b int) bool {
	return s.e.a.recs[s.e.a.refs[a]].id == s.e.b.recs[s.e.b.refs[b]].id
}

func (s *myersState) compare(off1, lim1, off2, lim2 int, needMin bool) error {
	if err := s.e.ctx.Err(); err != nil {
		return err
	}
	for off1 < lim1 && off2 < lim2 && s.equal(off1, off2) {
		off1++
		off2++
		if off1&1023 == 0 {
			if err := s.e.ctx.Err(); err != nil {
				return err
			}
		}
	}
	for off1 < lim1 && off2 < lim2 && s.equal(lim1-1, lim2-1) {
		lim1--
		lim2--
		if lim1&1023 == 0 {
			if err := s.e.ctx.Err(); err != nil {
				return err
			}
		}
	}
	if off1 == lim1 {
		for ; off2 < lim2; off2++ {
			if off2&1023 == 0 {
				if err := s.e.ctx.Err(); err != nil {
					return err
				}
			}
			s.e.b.setChanged(s.e.b.refs[off2], true)
		}
		return nil
	}
	if off2 == lim2 {
		for ; off1 < lim1; off1++ {
			if off1&1023 == 0 {
				if err := s.e.ctx.Err(); err != nil {
					return err
				}
			}
			s.e.a.setChanged(s.e.a.refs[off1], true)
		}
		return nil
	}
	split, err := s.split(off1, lim1, off2, lim2, needMin)
	if err != nil {
		return err
	}
	if split.a < off1 || split.a > lim1 || split.b < off2 || split.b > lim2 ||
		(split.a == off1 && split.b == off2) || (split.a == lim1 && split.b == lim2) {
		return fmt.Errorf("xdiff: invalid Myers split")
	}
	if err := s.compare(off1, split.a, off2, split.b, split.minLo); err != nil {
		return err
	}
	return s.compare(split.a, lim1, split.b, lim2, split.minHi)
}

func (s *myersState) split(off1, lim1, off2, lim2 int, needMin bool) (myersSplit, error) {
	dmin, dmax := off1-lim2, lim1-off2
	fmid, bmid := off1-off2, lim1-lim2
	odd := (fmid-bmid)&1 != 0
	fmin, fmax, bmin, bmax := fmid, fmid, bmid, bmid
	f, b, base := s.forward, s.backward, s.base
	lineMax := int(^uint(0) >> 1)
	f[fmid+base], b[bmid+base] = off1, lim1
	for ec := 1; ; ec++ {
		if err := s.e.ctx.Err(); err != nil {
			return myersSplit{}, err
		}
		gotSnake := false
		if fmin > dmin {
			fmin--
			f[fmin-1+base] = -1
		} else {
			fmin++
		}
		if fmax < dmax {
			fmax++
			f[fmax+1+base] = -1
		} else {
			fmax--
		}
		for d := fmax; d >= fmin; d -= 2 {
			if (fmax-d)&1023 == 0 {
				if err := s.e.ctx.Err(); err != nil {
					return myersSplit{}, err
				}
			}
			i1 := f[d+1+base]
			if f[d-1+base] >= f[d+1+base] {
				i1 = f[d-1+base] + 1
			}
			prev1, i2 := i1, i1-d
			for i1 < lim1 && i2 < lim2 && s.equal(i1, i2) {
				i1++
				i2++
				if i1&1023 == 0 {
					if err := s.e.ctx.Err(); err != nil {
						return myersSplit{}, err
					}
				}
			}
			if i1-prev1 > myersSnake {
				gotSnake = true
			}
			f[d+base] = i1
			if odd && bmin <= d && d <= bmax && b[d+base] <= i1 {
				return myersSplit{i1, i2, true, true}, nil
			}
		}
		if bmin > dmin {
			bmin--
			b[bmin-1+base] = lineMax
		} else {
			bmin++
		}
		if bmax < dmax {
			bmax++
			b[bmax+1+base] = lineMax
		} else {
			bmax--
		}
		for d := bmax; d >= bmin; d -= 2 {
			if (bmax-d)&1023 == 0 {
				if err := s.e.ctx.Err(); err != nil {
					return myersSplit{}, err
				}
			}
			i1 := b[d+1+base] - 1
			if b[d-1+base] < b[d+1+base] {
				i1 = b[d-1+base]
			}
			prev1, i2 := i1, i1-d
			for i1 > off1 && i2 > off2 && s.equal(i1-1, i2-1) {
				i1--
				i2--
				if i1&1023 == 0 {
					if err := s.e.ctx.Err(); err != nil {
						return myersSplit{}, err
					}
				}
			}
			if prev1-i1 > myersSnake {
				gotSnake = true
			}
			b[d+base] = i1
			if !odd && fmin <= d && d <= fmax && i1 <= f[d+base] {
				return myersSplit{i1, i2, true, true}, nil
			}
		}
		if needMin {
			continue
		}
		if gotSnake && ec > myersHeurMin {
			best := 0
			var split myersSplit
			for d := fmax; d >= fmin; d -= 2 {
				dd := d - fmid
				if dd < 0 {
					dd = -dd
				}
				i1 := f[d+base]
				i2 := i1 - d
				v := (i1 - off1) + (i2 - off2) - dd
				if v > myersKHeur*ec && v > best &&
					off1+myersSnake <= i1 && i1 < lim1 &&
					off2+myersSnake <= i2 && i2 < lim2 {
					for k := 1; s.equal(i1-k, i2-k); k++ {
						if k == myersSnake {
							best = v
							split = myersSplit{i1, i2, true, false}
							break
						}
					}
				}
			}
			if best > 0 {
				return split, nil
			}
			for d := bmax; d >= bmin; d -= 2 {
				dd := d - bmid
				if dd < 0 {
					dd = -dd
				}
				i1 := b[d+base]
				i2 := i1 - d
				v := (lim1 - i1) + (lim2 - i2) - dd
				if v > myersKHeur*ec && v > best &&
					off1 < i1 && i1 <= lim1-myersSnake &&
					off2 < i2 && i2 <= lim2-myersSnake {
					for k := 0; s.equal(i1+k, i2+k); k++ {
						if k == myersSnake-1 {
							best = v
							split = myersSplit{i1, i2, false, true}
							break
						}
					}
				}
			}
			if best > 0 {
				return split, nil
			}
		}
		if ec >= s.maxCost {
			fbest, fbest1 := -1, -1
			for d := fmax; d >= fmin; d -= 2 {
				i1 := min(f[d+base], lim1)
				i2 := i1 - d
				if lim2 < i2 {
					i1, i2 = lim2+d, lim2
				}
				if fbest < i1+i2 {
					fbest, fbest1 = i1+i2, i1
				}
			}
			bbest, bbest1 := lineMax, lineMax
			for d := bmax; d >= bmin; d -= 2 {
				i1 := max(off1, b[d+base])
				i2 := i1 - d
				if i2 < off2 {
					i1, i2 = off2+d, off2
				}
				if i1+i2 < bbest {
					bbest, bbest1 = i1+i2, i1
				}
			}
			if (lim1+lim2)-bbest < fbest-(off1+off2) {
				return myersSplit{fbest1, fbest - fbest1, true, false}, nil
			}
			return myersSplit{bbest1, bbest - bbest1, false, true}, nil
		}
	}
}
