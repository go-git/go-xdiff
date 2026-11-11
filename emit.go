// SPDX-License-Identifier: LGPL-2.1-or-later
// Copyright (C) 2003 Davide Libenzi.
// Go translation modified November 2026. See NOTICE.

package xdiff

import (
	"context"
	"errors"
	"io"
	"strconv"
)

const noNewlineMarker = "\n\\ No newline at end of file\n"

func appendRange(buf []byte, start, count int) []byte {
	if count != 0 {
		start++
	}
	buf = strconv.AppendInt(buf, int64(start), 10)
	if count != 1 {
		buf = append(buf, ',')
		buf = strconv.AppendInt(buf, int64(count), 10)
	}
	return buf
}

func hunkHeader(header []byte, h Hunk) []byte {
	header = append(header, "@@ -"...)
	header = appendRange(header, h.OldStart, h.OldLines)
	header = append(header, " +"...)
	header = appendRange(header, h.NewStart, h.NewLines)
	header = append(header, " @@"...)
	if h.Function != "" {
		header = append(header, ' ')
		header = append(header, h.Function[:min(len(h.Function), 128-len(header)-1)]...)
	}
	return append(header, '\n')
}

// Emit sends unified output without file headers to callbacks.
// It requires a non-nil LineFunc, even when the result has no changes.
// It checks cancellation before and after each callback.
func (r *Result) Emit(ctx context.Context, options *EmitOptions, callbacks *EmitCallbacks) error {
	opts, err := emitOptions(ctx, options)
	if err != nil {
		return err
	}
	if callbacks == nil || callbacks.LineFunc == nil {
		return errors.New("xdiff: nil line callback")
	}
	headerFunc, lineFunc := callbacks.HunkHeaderFunc, callbacks.LineFunc
	var parts [3][]byte
	var prefix [1]byte
	var header []byte
	var marker []byte
	return r.eachHunk(ctx, opts, func(h Hunk) error {
		if !opts.NoHunkHeader {
			if err := ctx.Err(); err != nil {
				return err
			}
			var err error
			if headerFunc != nil {
				err = headerFunc(h)
			} else {
				if header == nil {
					header = make([]byte, 0, 128)
				}
				header = hunkHeader(header[:0], h)
				parts[0] = header
				err = lineFunc(parts[:1]...)
			}
			if err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		for _, line := range h.Lines {
			if err := ctx.Err(); err != nil {
				return err
			}
			prefix[0] = ' '
			switch line.Operation {
			case Delete:
				prefix[0] = '-'
			case Insert:
				prefix[0] = '+'
			}
			parts[0], parts[1] = prefix[:], line.Content
			count := 2
			if len(line.Content) > 0 && line.Content[len(line.Content)-1] != '\n' {
				if marker == nil {
					marker = []byte(noNewlineMarker)
				}
				parts[2] = marker
				count++
			}
			if err := lineFunc(parts[:count]...); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
		}
		return nil
	})
}

// WriteUnified writes unified hunks without file headers through result.Emit.
// It returns writer errors and reports incomplete writes as io.ErrShortWrite.
func WriteUnified(ctx context.Context, w io.Writer, result *Result, options *EmitOptions) error {
	if w == nil {
		return errors.New("xdiff: nil writer")
	}
	return result.Emit(ctx, options, &EmitCallbacks{
		LineFunc: func(parts ...[]byte) error {
			// Emit checks cancellation before the first part and after the last.
			for i, part := range parts {
				if i > 0 {
					if err := ctx.Err(); err != nil {
						return err
					}
				}
				n, err := w.Write(part)
				if err != nil {
					return err
				}
				if n != len(part) {
					return io.ErrShortWrite
				}
			}
			return nil
		},
	})
}
