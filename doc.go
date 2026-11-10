// Package xdiff implements Git's line-oriented diff and three-way merge
// algorithms in pure Go.
//
// Inputs are arbitrary bytes. A record ends at '\n' or at the end of the input.
// The final newline is significant unless a whitespace option ignores it.
// Diff results use zero-based line offsets and retain references to the inputs.
// Callers must not change the inputs while they use a result.
//
// Options are read-only. Independent calls are safe for concurrent use.
// Cancellation returns the context error, not a partial result.
//
// The implementation derives from Git's xdiff. See LICENSE and NOTICE.
package xdiff
