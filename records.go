// SPDX-License-Identifier: LGPL-2.1-or-later
// Copyright (C) 2003 Davide Libenzi.
// Go translation modified November 2026. See NOTICE.

package xdiff

import (
	"bytes"
	"context"
	"hash/maphash"
	"math/bits"
	"slices"
)

type record struct {
	id    int
	start int
}

type file struct {
	data       []byte
	recs       []record
	changed    []bool
	refs       []int
	start, end int
	groups     int
}

func (f *file) isChanged(i int) bool { return f.changed[i+1] }
func (f *file) setChanged(i int, value bool) {
	if f.isChanged(i) == value {
		return
	}
	delta := 1
	if f.changed[i] {
		delta--
	}
	if f.changed[i+2] {
		delta--
	}
	if !value {
		delta = -delta
	}
	f.groups += delta
	f.changed[i+1] = value
}

func (f *file) region(start, count int) []byte {
	begin := len(f.data)
	if start < len(f.recs) {
		begin = f.recs[start].start
	}
	end := len(f.data)
	if start+count < len(f.recs) {
		end = f.recs[start+count].start
	}
	return f.data[begin:end]
}

// Git's sane-ctype table deliberately excludes form feed and vertical tab.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func comparisonBytes(key, line []byte, mode Whitespace) []byte {
	if mode == 0 {
		return line
	}
	switch {
	case mode&IgnoreAllSpace != 0:
		for _, c := range line {
			if !isSpace(c) {
				key = append(key, c)
			}
		}
		return key
	case mode&IgnoreSpaceChange != 0:
		space := false
		for _, c := range line {
			if isSpace(c) {
				space = true
				continue
			}
			if space {
				key = append(key, ' ')
			}
			space = false
			key = append(key, c)
		}
		return key
	case mode&IgnoreSpaceAtEOL != 0:
		for len(line) > 0 && isSpace(line[len(line)-1]) {
			line = line[:len(line)-1]
		}
		return line
	default:
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
		}
		return line
	}
}

type lineClass struct {
	key  []byte
	hash uint64
}

type classifier struct {
	buckets []int
	entries []lineClass
	seed    maphash.Seed
	buffer  []byte
	keys    []byte
	last    int
}

func (c *classifier) intern(hash uint64, key []byte, count int, copyKey bool) int {
	slot := c.slot(hash, key)
	if id := c.buckets[slot]; id != 0 {
		return id - 1
	}
	id := len(c.entries)
	if id == 8 || id*2 >= len(c.buckets) {
		// Leave room for new classes from the second input.
		capacity := count + count/8
		size := len(c.buckets) * 2
		for size < capacity*2 {
			size *= 2
		}
		c.buckets = make([]int, size)
		for i, entry := range c.entries {
			c.buckets[c.slot(entry.hash, entry.key)] = i + 1
		}
		c.entries = slices.Grow(c.entries, max(0, capacity-len(c.entries)))
		slot = c.slot(hash, key)
	}
	if copyKey {
		// Keep normalized keys in blocks instead of allocating each key.
		if cap(c.keys)-len(c.keys) < len(key) {
			c.keys = make([]byte, 0, max(4096, len(key)))
		}
		start := len(c.keys)
		c.keys = append(c.keys, key...)
		key = c.keys[start:len(c.keys):len(c.keys)]
	}
	if id == cap(c.entries) {
		c.entries = slices.Grow(c.entries, max(id, count))
	}
	c.entries = append(c.entries, lineClass{key: key, hash: hash})
	c.buckets[slot] = id + 1
	return id
}

func (c *classifier) slot(hash uint64, key []byte) int {
	mask := len(c.buckets) - 1
	slot := int(hash) & mask
	for c.buckets[slot] != 0 {
		entry := &c.entries[c.buckets[slot]-1]
		if entry.hash == hash && bytes.Equal(entry.key, key) {
			break
		}
		slot = (slot + 1) & mask
	}
	return slot
}

// A nil classifier builds only line offsets for completed simple diffs.
func readFile(ctx context.Context, data []byte, mode Whitespace, classes *classifier) (file, error) {
	count := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		count++
	}
	f := file{data: data, recs: make([]record, 0, count)}
	if classes != nil && classes.buckets == nil {
		classes.buckets = make([]int, 16)
		classes.seed = maphash.MakeSeed()
		classes.entries = make([]lineClass, 0, min(count, 8))
	}
	for start := 0; start < len(data); {
		if err := ctx.Err(); err != nil {
			return file{}, err
		}
		end := len(data)
		if i := bytes.IndexByte(data[start:], '\n'); i >= 0 {
			end = start + i + 1
		}
		line := data[start:end]
		id := 0
		if classes != nil {
			key, copyKey := line, false
			if mode != 0 {
				copyKey = mode&(IgnoreAllSpace|IgnoreSpaceChange) != 0
				if copyKey {
					classes.buffer = slices.Grow(classes.buffer[:0], len(line))
				}
				key = comparisonBytes(classes.buffer[:0], line, mode)
				if copyKey {
					classes.buffer = key
				}
			}
			id = classes.last
			// A repeated long record is cheaper to compare than to hash again.
			if len(key) <= 64 || len(classes.entries) == 0 || !bytes.Equal(classes.entries[id].key, key) {
				hash := maphash.Bytes(classes.seed, key)
				id = classes.buckets[classes.slot(hash, key)] - 1
				if id < 0 {
					id = classes.intern(hash, key, count, copyKey)
				}
				classes.last = id
			}
		}
		f.recs = append(f.recs, record{id: id, start: start})
		start = end
	}
	if classes != nil {
		f.changed = make([]bool, len(f.recs)+2)
	}
	f.end = len(f.recs) - 1
	return f, nil
}

func bogosqrt(n int) int {
	return 1 << ((bits.Len(uint(n)) + 1) / 2)
}
