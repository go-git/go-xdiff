// SPDX-License-Identifier: LGPL-2.1-or-later
// Copyright (C) 2003-2006 Davide Libenzi, Johannes E. Schindelin.
// Go translation modified November 2026. See NOTICE.
//
// Port of Git xdiff/xmerge.c.

package xdiff

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type mergeChange struct {
	next             *mergeChange
	mode             int
	i0, i1, i2       int
	chg0, chg1, chg2 int
}

type mergeList struct {
	head, tail *mergeChange
}

func (l *mergeList) append(mode, i0, chg0, i1, chg1, i2, chg2 int) {
	m := l.tail
	if m != nil && (i1 <= m.i1+m.chg1 || i2 <= m.i2+m.chg2) {
		if mode != m.mode {
			m.mode = 0
		}
		m.chg0 = i0 + chg0 - m.i0
		m.chg1 = i1 + chg1 - m.i1
		m.chg2 = i2 + chg2 - m.i2
		return
	}
	m = &mergeChange{mode: mode, i0: i0, chg0: chg0,
		i1: i1, chg1: chg1, i2: i2, chg2: chg2}
	if l.tail == nil {
		l.head = m
	} else {
		l.tail.next = m
	}
	l.tail = m
}

type merger struct {
	ctx          context.Context
	ours, theirs *Result
	opts         MergeOptions
}

// Merge computes a three-way merge without changing its inputs or options.
// Nil options select MergeZealous and seven-character conflict markers.
// A conflict is not an error. Content is independent of the input slices.
// Labels retain embedded newlines; a NUL terminates a label, as in Git.
func Merge(ctx context.Context, base, ours, theirs []byte, options *MergeOptions) (*MergeResult, error) {
	if ctx == nil {
		return nil, errors.New("xdiff: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opts := MergeOptions{Level: MergeZealous}
	if options != nil {
		opts = *options
	}
	if err := validateOptions(opts.Diff); err != nil {
		return nil, err
	}
	if opts.Level > MergeZealousAlnum {
		return nil, fmt.Errorf("xdiff: invalid merge level %d", opts.Level)
	}
	if opts.Favor > FavorUnion {
		return nil, fmt.Errorf("xdiff: invalid merge favor %d", opts.Favor)
	}
	if opts.Style > ConflictZDiff3 {
		return nil, fmt.Errorf("xdiff: invalid conflict style %d", opts.Style)
	}
	if opts.MarkerSize < 0 {
		return nil, errors.New("xdiff: negative conflict marker size")
	}
	if opts.MarkerSize == 0 {
		opts.MarkerSize = 7
	}
	if opts.Style != ConflictMerge && opts.Level > MergeEager {
		opts.Level = MergeEager
	}
	// Diff retains the raw script even when output suppression is enabled.
	left, err := Diff(ctx, base, ours, &opts.Diff)
	if err != nil {
		return nil, err
	}
	right, err := Diff(ctx, base, theirs, &opts.Diff)
	if err != nil {
		return nil, err
	}
	output := mergeOutput{ctx: ctx}
	if len(left.changes) == 0 || len(right.changes) == 0 {
		data := ours
		if len(left.changes) == 0 {
			data = theirs
		}
		if err := output.copy(data); err != nil {
			return nil, err
		}
		return &MergeResult{Content: output.data}, nil
	}
	m := merger{ctx: ctx, ours: left, theirs: right, opts: opts}
	changes, err := m.changes()
	if err != nil {
		return nil, err
	}
	if opts.Style == ConflictZDiff3 {
		err = m.refineZDiff3(changes)
	} else if opts.Level >= MergeZealous {
		err = m.refine(changes)
		if err == nil {
			err = m.simplify(changes)
		}
	}
	if err != nil {
		return nil, err
	}
	return m.output(changes, &output)
}

func (m *merger) equalLines(i1, i2, count int) (bool, error) {
	for i := 0; i < count; i++ {
		if err := m.ctx.Err(); err != nil {
			return false, err
		}
		if comparisonKey(m.ours.b.region(i1+i, 1), m.opts.Diff.Whitespace) !=
			comparisonKey(m.theirs.b.region(i2+i, 1), m.opts.Diff.Whitespace) {
			return false, nil
		}
	}
	return true, nil
}

func (m *merger) changes() (*mergeChange, error) {
	left, right := m.ours.changes, m.theirs.changes
	var list mergeList
	for len(left) > 0 && len(right) > 0 {
		if err := m.ctx.Err(); err != nil {
			return nil, err
		}
		a, b := left[0], right[0]
		if a.OldStart+a.OldLines < b.OldStart {
			list.append(1, a.OldStart, a.OldLines, a.NewStart, a.NewLines,
				b.NewStart-b.OldStart+a.OldStart, a.OldLines)
			left = left[1:]
			continue
		}
		if b.OldStart+b.OldLines < a.OldStart {
			list.append(2, b.OldStart, b.OldLines,
				a.NewStart-a.OldStart+b.OldStart, b.OldLines, b.NewStart, b.NewLines)
			right = right[1:]
			continue
		}
		identical := false
		if m.opts.Level != MergeMinimal && a.OldStart == b.OldStart &&
			a.OldLines == b.OldLines && a.NewLines == b.NewLines {
			var err error
			identical, err = m.equalLines(a.NewStart, b.NewStart, a.NewLines)
			if err != nil {
				return nil, err
			}
		}
		if !identical {
			off := a.OldStart - b.OldStart
			ffo := off + a.OldLines - b.OldLines
			i0, i1, i2 := a.OldStart, a.NewStart, b.NewStart
			if off > 0 {
				i0 -= off
				i1 -= off
			} else {
				i2 += off
			}
			chg0 := a.OldStart + a.OldLines - i0
			chg1 := a.NewStart + a.NewLines - i1
			chg2 := b.NewStart + b.NewLines - i2
			if ffo < 0 {
				chg0 -= ffo
				chg1 -= ffo
			} else {
				chg2 += ffo
			}
			list.append(0, i0, chg0, i1, chg1, i2, chg2)
		}
		end1, end2 := a.OldStart+a.OldLines, b.OldStart+b.OldLines
		if end1 >= end2 {
			right = right[1:]
		}
		if end2 >= end1 {
			left = left[1:]
		}
	}
	for _, a := range left {
		if err := m.ctx.Err(); err != nil {
			return nil, err
		}
		list.append(1, a.OldStart, a.OldLines, a.NewStart, a.NewLines,
			a.OldStart+len(m.theirs.b.recs)-len(m.theirs.a.recs), a.OldLines)
	}
	for _, b := range right {
		if err := m.ctx.Err(); err != nil {
			return nil, err
		}
		list.append(2, b.OldStart, b.OldLines,
			b.OldStart+len(m.ours.b.recs)-len(m.ours.a.recs), b.OldLines, b.NewStart, b.NewLines)
	}
	return list.head, nil
}

func (m *merger) refineZDiff3(c *mergeChange) error {
	for ; c != nil; c = c.next {
		if err := m.ctx.Err(); err != nil {
			return err
		}
		if c.mode != 0 {
			continue
		}
		for c.chg1 > 0 && c.chg2 > 0 {
			equal, err := m.equalLines(c.i1, c.i2, 1)
			if err != nil {
				return err
			}
			if !equal {
				break
			}
			c.chg1--
			c.chg2--
			c.i1++
			c.i2++
		}
		for c.chg1 > 0 && c.chg2 > 0 {
			equal, err := m.equalLines(c.i1+c.chg1-1, c.i2+c.chg2-1, 1)
			if err != nil {
				return err
			}
			if !equal {
				break
			}
			c.chg1--
			c.chg2--
		}
	}
	return nil
}

func (m *merger) refine(c *mergeChange) error {
	for ; c != nil; c = c.next {
		if err := m.ctx.Err(); err != nil {
			return err
		}
		if c.mode != 0 || c.chg1 == 0 || c.chg2 == 0 {
			continue
		}
		i1, i2 := c.i1, c.i2
		r, err := Diff(m.ctx, m.ours.b.region(i1, c.chg1),
			m.theirs.b.region(i2, c.chg2), &m.opts.Diff)
		if err != nil {
			return err
		}
		if len(r.changes) == 0 {
			// Mode 4 omits this node; the unchanged ours region remains.
			c.mode = 4
			continue
		}
		for i, change := range r.changes {
			if err := m.ctx.Err(); err != nil {
				return err
			}
			if i > 0 {
				next := &mergeChange{next: c.next}
				c.next = next
				c = next
			}
			c.i1, c.chg1 = i1+change.OldStart, change.OldLines
			c.i2, c.chg2 = i2+change.NewStart, change.NewLines
		}
	}
	return nil
}

func (m *merger) containsAlnum(start, count int) (bool, error) {
	for line := start; line < start+count; line++ {
		for i, c := range m.ours.b.region(line, 1) {
			if i%65536 == 0 {
				if err := m.ctx.Err(); err != nil {
					return false, err
				}
			}
			if c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
				return true, nil
			}
		}
	}
	return false, nil
}

func (m *merger) simplify(c *mergeChange) error {
	for c != nil && c.next != nil {
		if err := m.ctx.Err(); err != nil {
			return err
		}
		next := c.next
		begin, end := c.i1+c.chg1, next.i1
		separate := c.mode != 0 || next.mode != 0
		if !separate && end-begin > 3 {
			separate = m.opts.Level <= MergeZealous
			if !separate {
				var err error
				separate, err = m.containsAlnum(begin, end-begin)
				if err != nil {
					return err
				}
			}
		}
		if separate {
			c = next
		} else {
			c.chg1 = next.i1 + next.chg1 - c.i1
			c.chg2 = next.i2 + next.chg2 - c.i2
			c.next = next.next
		}
	}
	return nil
}

// The return value is -1 when the file has no determinable line ending.
func mergeEOLCRLF(f *file, i int) int {
	if len(f.recs) == 0 {
		return -1
	}
	line := f.region(i, 1)
	if i == len(f.recs)-1 && (len(line) == 0 || line[len(line)-1] != '\n') {
		if i == 0 {
			return -1
		}
		line = f.region(i-1, 1)
	}
	if len(line) > 1 && line[len(line)-2] == '\r' {
		return 1
	}
	return 0
}

func (m *merger) needsCR(c *mergeChange) bool {
	cr := mergeEOLCRLF(&m.ours.b, max(c.i1-1, 0))
	if cr != 0 {
		cr = mergeEOLCRLF(&m.theirs.b, max(c.i2-1, 0))
	}
	if cr != 0 {
		cr = mergeEOLCRLF(&m.ours.a, 0)
	}
	return cr > 0
}

type mergeOutput struct {
	ctx  context.Context
	data []byte
}

func (o *mergeOutput) copy(data []byte) error {
	for len(data) > 0 {
		if err := o.ctx.Err(); err != nil {
			return err
		}
		n := min(len(data), 65536)
		o.data = append(o.data, data[:n]...)
		data = data[n:]
	}
	return o.ctx.Err()
}

func (o *mergeOutput) records(f *file, start, count int, cr, addNL bool) error {
	if count < 1 {
		return o.ctx.Err()
	}
	data := f.region(start, count)
	if err := o.copy(data); err != nil {
		return err
	}
	if addNL && (len(data) == 0 || data[len(data)-1] != '\n') {
		if cr {
			o.data = append(o.data, '\r')
		}
		o.data = append(o.data, '\n')
	}
	return nil
}

func (o *mergeOutput) marker(c byte, size int, label string, cr bool) error {
	var block [256]byte
	for i := range block {
		block[i] = c
	}
	for size > 0 {
		n := min(size, len(block))
		if err := o.copy(block[:n]); err != nil {
			return err
		}
		size -= n
	}
	if i := strings.IndexByte(label, 0); i >= 0 {
		label = label[:i]
	}
	if label != "" {
		o.data = append(o.data, ' ')
		// Copy strings in bounded pieces to preserve cancellation checks.
		for len(label) > 0 {
			n := min(len(label), 65536)
			if err := o.copy([]byte(label[:n])); err != nil {
				return err
			}
			label = label[n:]
		}
	}
	if cr {
		o.data = append(o.data, '\r')
	}
	o.data = append(o.data, '\n')
	return nil
}

func (m *merger) output(changes *mergeChange, o *mergeOutput) (*MergeResult, error) {
	conflicts, i := 0, 0
	for c := changes; c != nil; c = c.next {
		if err := m.ctx.Err(); err != nil {
			return nil, err
		}
		mode := c.mode
		if mode == 0 {
			mode = int(m.opts.Favor)
		}
		if mode != 0 && mode&3 == 0 {
			continue
		}
		if err := o.records(&m.ours.b, i, c.i1-i, false, false); err != nil {
			return nil, err
		}
		if mode == 0 {
			conflicts++
			if err := m.conflict(c, o); err != nil {
				return nil, err
			}
		} else {
			if mode&1 != 0 {
				if err := o.records(&m.ours.b, c.i1, c.chg1, m.needsCR(c), mode&2 != 0); err != nil {
					return nil, err
				}
			}
			if mode&2 != 0 {
				if err := o.records(&m.theirs.b, c.i2, c.chg2, false, false); err != nil {
					return nil, err
				}
			}
		}
		i = c.i1 + c.chg1
	}
	if err := o.records(&m.ours.b, i, len(m.ours.b.recs)-i, false, false); err != nil {
		return nil, err
	}
	if err := m.ctx.Err(); err != nil {
		return nil, err
	}
	return &MergeResult{Content: o.data, Conflicts: conflicts}, nil
}

func (m *merger) conflict(c *mergeChange, o *mergeOutput) error {
	cr := m.needsCR(c)
	if err := o.marker('<', m.opts.MarkerSize, m.opts.OursLabel, cr); err != nil {
		return err
	}
	if err := o.records(&m.ours.b, c.i1, c.chg1, cr, true); err != nil {
		return err
	}
	if m.opts.Style != ConflictMerge {
		if err := o.marker('|', m.opts.MarkerSize, m.opts.AncestorLabel, cr); err != nil {
			return err
		}
		if err := o.records(&m.ours.a, c.i0, c.chg0, cr, true); err != nil {
			return err
		}
	}
	if err := o.marker('=', m.opts.MarkerSize, "", cr); err != nil {
		return err
	}
	if err := o.records(&m.theirs.b, c.i2, c.chg2, cr, true); err != nil {
		return err
	}
	return o.marker('>', m.opts.MarkerSize, m.opts.TheirsLabel, cr)
}
