// SPDX-License-Identifier: LGPL-2.1-or-later
// Copyright (C) 2003 Davide Libenzi.
// Go translation modified November 2026. See NOTICE.

package xdiff

import (
	"bytes"
	"context"
	"maps"
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
}

func (f *file) isChanged(i int) bool { return f.changed[i+1] }
func (f *file) setChanged(i int, value bool) {
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

func comparisonKey(line []byte, mode Whitespace) string {
	if mode == 0 {
		return string(line)
	}
	switch {
	case mode&IgnoreAllSpace != 0:
		key := make([]byte, 0, len(line))
		for _, c := range line {
			if !isSpace(c) {
				key = append(key, c)
			}
		}
		return string(key)
	case mode&IgnoreSpaceChange != 0:
		key := make([]byte, 0, len(line))
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
		return string(key)
	case mode&IgnoreSpaceAtEOL != 0:
		for len(line) > 0 && isSpace(line[len(line)-1]) {
			line = line[:len(line)-1]
		}
		return string(line)
	default:
		if len(line) > 0 && line[len(line)-1] == '\n' {
			line = line[:len(line)-1]
			if len(line) > 0 && line[len(line)-1] == '\r' {
				line = line[:len(line)-1]
			}
		}
		return string(line)
	}
}

func readFile(ctx context.Context, data []byte, mode Whitespace, classes *map[string]int) (file, error) {
	count := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		count++
	}
	f := file{data: data, recs: make([]record, 0, count)}
	var text string
	if mode == 0 {
		// Substrings share one copy instead of allocating a key for each record.
		text = string(data)
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
		var key string
		if mode == 0 {
			key = text[start:end]
		} else {
			key = comparisonKey(line, mode)
		}
		id, found := (*classes)[key]
		if !found {
			// Keep small class sets small, even when the file has many records.
			if len(*classes) == 8 {
				grown := make(map[string]int, count)
				maps.Copy(grown, *classes)
				*classes = grown
			}
			id = len(*classes)
			(*classes)[key] = id
		}
		f.recs = append(f.recs, record{id: id, start: start})
		start = end
	}
	f.changed = make([]bool, len(f.recs)+2)
	f.end = len(f.recs) - 1
	return f, nil
}

func bogosqrt(n int) int {
	result := 1
	for n > 0 {
		result <<= 1
		n >>= 2
	}
	return result
}
