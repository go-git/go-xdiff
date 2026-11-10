// SPDX-License-Identifier: LGPL-2.1-or-later
// Copyright (C) 2003 Davide Libenzi
// Go translation modified November 2026.
// Port of xdiff/xdiffi.c.

package xdiff

import "fmt"

const (
	maxIndent        = 200
	maxBlanks        = 20
	indentMaxSliding = 100
)

func recordIndent(line []byte) int {
	indent := 0
	for _, c := range line {
		if !isSpace(c) {
			return indent
		}
		switch c {
		case ' ':
			indent++
		case '\t':
			indent += 8 - indent%8
		}
		if indent >= maxIndent {
			return maxIndent
		}
	}
	return -1
}

type splitMeasurement struct {
	endOfFile                                          bool
	indent, preBlank, preIndent, postBlank, postIndent int
}

type splitScore struct {
	effectiveIndent, penalty int
}

func measureSplit(f *file, split int) splitMeasurement {
	m := splitMeasurement{indent: -1, preIndent: -1, postIndent: -1}
	if split >= len(f.recs) {
		m.endOfFile = true
	} else {
		m.indent = recordIndent(f.region(split, 1))
	}
	for i := split - 1; i >= 0; i-- {
		m.preIndent = recordIndent(f.region(i, 1))
		if m.preIndent != -1 {
			break
		}
		m.preBlank++
		if m.preBlank == maxBlanks {
			m.preIndent = 0
			break
		}
	}
	for i := split + 1; i < len(f.recs); i++ {
		m.postIndent = recordIndent(f.region(i, 1))
		if m.postIndent != -1 {
			break
		}
		m.postBlank++
		if m.postBlank == maxBlanks {
			m.postIndent = 0
			break
		}
	}
	return m
}

func (s *splitScore) add(m splitMeasurement) {
	if m.preIndent == -1 && m.preBlank == 0 {
		s.penalty++
	}
	if m.endOfFile {
		s.penalty += 21
	}
	postBlank := 0
	if m.indent == -1 {
		postBlank = 1 + m.postBlank
	}
	totalBlank := m.preBlank + postBlank
	s.penalty += -30*totalBlank + 6*postBlank
	indent := m.indent
	if indent == -1 {
		indent = m.postIndent
	}
	s.effectiveIndent += indent
	if indent == -1 || m.preIndent == -1 || indent == m.preIndent {
		return
	}
	if indent > m.preIndent {
		if totalBlank != 0 {
			s.penalty += 10
		} else {
			s.penalty -= 4
		}
	} else if m.postIndent != -1 && m.postIndent > indent {
		if totalBlank != 0 {
			s.penalty += 17
		} else {
			s.penalty += 24
		}
	} else {
		if totalBlank != 0 {
			s.penalty += 17
		} else {
			s.penalty += 23
		}
	}
}

func (s splitScore) compare(other splitScore) int {
	cmp := 0
	if s.effectiveIndent > other.effectiveIndent {
		cmp = 1
	} else if s.effectiveIndent < other.effectiveIndent {
		cmp = -1
	}
	return 60*cmp + s.penalty - other.penalty
}

type changeGroup struct {
	start, end int
}

func firstGroup(f *file) changeGroup {
	g := changeGroup{}
	for f.isChanged(g.end) {
		g.end++
	}
	return g
}

func (g *changeGroup) next(f *file) bool {
	if g.end == len(f.recs) {
		return false
	}
	g.start = g.end + 1
	g.end = g.start
	for f.isChanged(g.end) {
		g.end++
	}
	return true
}

func (g *changeGroup) previous(f *file) bool {
	if g.start == 0 {
		return false
	}
	g.end = g.start - 1
	g.start = g.end
	for f.isChanged(g.start - 1) {
		g.start--
	}
	return true
}

func (g *changeGroup) slideDown(f *file) bool {
	if g.end >= len(f.recs) || f.recs[g.start].id != f.recs[g.end].id {
		return false
	}
	f.setChanged(g.start, false)
	g.start++
	f.setChanged(g.end, true)
	g.end++
	for f.isChanged(g.end) {
		g.end++
	}
	return true
}

func (g *changeGroup) slideUp(f *file) bool {
	if g.start == 0 || f.recs[g.start-1].id != f.recs[g.end-1].id {
		return false
	}
	g.start--
	f.setChanged(g.start, true)
	g.end--
	f.setChanged(g.end, false)
	for f.isChanged(g.start - 1) {
		g.start--
	}
	return true
}

func (e *environment) compact() error {
	if err := e.ctx.Err(); err != nil {
		return err
	}
	if err := validateChanges(&e.a); err != nil {
		return err
	}
	if err := validateChanges(&e.b); err != nil {
		return err
	}
	if err := e.compactFile(&e.a, &e.b); err != nil {
		return err
	}
	return e.compactFile(&e.b, &e.a)
}

func (e *environment) compactFile(f, other *file) error {
	g, goOther := firstGroup(f), firstGroup(other)
	for {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		if g.end != g.start {
			original := g
			var groupSize, earliestEnd, endMatchingOther int
			for {
				groupSize = g.end - g.start
				endMatchingOther = -1
				for g.slideUp(f) {
					if err := e.ctx.Err(); err != nil {
						return err
					}
					if !goOther.previous(other) {
						return fmt.Errorf("xdiff: group sync broken sliding up")
					}
				}
				earliestEnd = g.end
				if goOther.end > goOther.start {
					endMatchingOther = g.end
				}
				for g.slideDown(f) {
					if err := e.ctx.Err(); err != nil {
						return err
					}
					if !goOther.next(other) {
						return fmt.Errorf("xdiff: group sync broken sliding down")
					}
					if goOther.end > goOther.start {
						endMatchingOther = g.end
					}
				}
				if groupSize == g.end-g.start {
					break
				}
			}
			if g.end != earliestEnd {
				if endMatchingOther != -1 {
					for goOther.end == goOther.start {
						if err := e.ctx.Err(); err != nil {
							return err
						}
						if !g.slideUp(f) {
							return fmt.Errorf("xdiff: matching group disappeared")
						}
						if !goOther.previous(other) {
							return fmt.Errorf("xdiff: group sync broken sliding to match")
						}
					}
				} else if e.opts.IndentHeuristic {
					bestShift := -1
					var bestScore splitScore
					shift := max(earliestEnd, g.end-groupSize-1, g.end-indentMaxSliding)
					for ; shift <= g.end; shift++ {
						if err := e.ctx.Err(); err != nil {
							return err
						}
						var score splitScore
						score.add(measureSplit(f, shift))
						score.add(measureSplit(f, shift-groupSize))
						if bestShift == -1 || score.compare(bestScore) <= 0 {
							bestScore, bestShift = score, shift
						}
					}
					for g.end > bestShift {
						if !g.slideUp(f) {
							return fmt.Errorf("xdiff: best group shift unreached")
						}
						if !goOther.previous(other) {
							return fmt.Errorf("xdiff: group sync broken sliding to blank line")
						}
					}
				}
			}
			if goOther.end != goOther.start && e.opts.Algorithm == Histogram && g != original {
				opts := e.opts
				opts.Algorithm = Myers
				if err := fallbackDiff(e.ctx, f, g.start, g.end-g.start,
					other, goOther.start, goOther.end-goOther.start, opts); err != nil {
					return err
				}
			}
		}
		if !g.next(f) {
			break
		}
		if !goOther.next(other) {
			return fmt.Errorf("xdiff: group sync broken moving to next group")
		}
	}
	if goOther.next(other) {
		return fmt.Errorf("xdiff: group sync broken at end of file")
	}
	return nil
}

func (e *environment) script() []Change {
	count := max(e.a.groups, e.b.groups)
	if count == 0 {
		return nil
	}
	changes := make([]Change, 0, count)
	for i1, i2 := len(e.a.recs), len(e.b.recs); i1 >= 0 || i2 >= 0; i1, i2 = i1-1, i2-1 {
		if e.a.isChanged(i1-1) || e.b.isChanged(i2-1) {
			l1, l2 := i1, i2
			for e.a.isChanged(i1 - 1) {
				i1--
			}
			for e.b.isChanged(i2 - 1) {
				i2--
			}
			changes = append(changes, Change{OldStart: i1, OldLines: l1 - i1, NewStart: i2, NewLines: l2 - i2})
		}
	}
	for i, j := 0, len(changes)-1; i < j; i, j = i+1, j-1 {
		changes[i], changes[j] = changes[j], changes[i]
	}
	return changes
}
