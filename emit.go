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

func hunkHeader(h Hunk) []byte {
	header := appendRange([]byte("@@ -"), h.OldStart, h.OldLines)
	header = append(header, " +"...)
	header = appendRange(header, h.NewStart, h.NewLines)
	header = append(header, " @@"...)
	if h.Function != "" {
		header = append(header, ' ')
		header = append(header, h.Function[:min(len(h.Function), 128-len(header)-1)]...)
	}
	return append(header, '\n')
}

// WriteUnified writes unified hunks without file headers.
// It returns writer errors and reports incomplete writes as io.ErrShortWrite.
func (r *Result) WriteUnified(ctx context.Context, w io.Writer, options *EmitOptions) error {
	opts, err := emitOptions(ctx, options)
	if err != nil {
		return err
	}
	if w == nil {
		return errors.New("xdiff: nil writer")
	}
	write := func(data []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n != len(data) {
			return io.ErrShortWrite
		}
		return ctx.Err()
	}
	return r.eachHunk(ctx, opts, func(h Hunk) error {
		if !opts.NoHunkHeader {
			if err := write(hunkHeader(h)); err != nil {
				return err
			}
		}
		for _, line := range h.Lines {
			prefix := byte(' ')
			switch line.Operation {
			case Delete:
				prefix = '-'
			case Insert:
				prefix = '+'
			}
			if err := write([]byte{prefix}); err != nil {
				return err
			}
			if err := write(line.Content); err != nil {
				return err
			}
			if len(line.Content) > 0 && line.Content[len(line.Content)-1] != '\n' {
				if err := write([]byte("\n\\ No newline at end of file\n")); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
