package xdiff

import "regexp"

// Algorithm selects a line correspondence algorithm.
type Algorithm uint8

const (
	Myers Algorithm = iota
	Patience
	Histogram
)

// Whitespace selects line comparison rules. The zero value compares all bytes.
// If several flags are set, the first applicable rule in this list takes
// precedence: IgnoreAllSpace, IgnoreSpaceChange, IgnoreSpaceAtEOL, IgnoreCRAtEOL.
type Whitespace uint8

const (
	IgnoreAllSpace Whitespace = 1 << iota
	IgnoreSpaceChange
	IgnoreSpaceAtEOL
	IgnoreCRAtEOL
)

// Options controls diff computation. A nil pointer selects the zero values.
type Options struct {
	Algorithm       Algorithm
	Minimal         bool
	IndentHeuristic bool
	Whitespace      Whitespace
	// IgnoreBlankLines suppresses changes that contain only blank lines.
	// It affects hunks and output, not the underlying edit script.
	IgnoreBlankLines bool
	// IgnoreRegex suppresses changes whose lines all match at least one
	// expression. Expressions match full records, including final newlines.
	// Expressions use Go regexp semantics. Use (?m) for Git-like line anchors.
	// CompilePOSIX provides POSIX ERE syntax, but does not reproduce C's
	// REG_NEWLINE flag or locale-dependent rules.
	IgnoreRegex []*regexp.Regexp
	// Anchors are line prefixes that constrain patience diff.
	Anchors []string
}

// Change describes a replacement. Offsets are zero-based line indexes.
// A zero count represents an insertion or deletion at the specified offset.
type Change struct {
	OldStart, OldLines int
	NewStart, NewLines int
}

// Operation identifies a chunk's role.
type Operation int8

const (
	Delete Operation = -1
	Equal  Operation = 0
	Insert Operation = 1
)

// Chunk contains input slices for one operation. Equal chunks retain both
// inputs because whitespace rules can match lines with different bytes.
// Delete chunks have no New bytes. Insert chunks have no Old bytes.
type Chunk struct {
	Operation Operation
	Old, New  []byte
}

// EmitOptions controls hunk construction and emission. Zero values request no context.
type EmitOptions struct {
	Context, InterHunkContext int
	NoHunkHeader              bool
	FunctionNames             bool
	FunctionContext           bool
	// FindFunc identifies a function header and its display name.
	// A nil callback uses xdiff's default ASCII header detection.
	FindFunc func(line []byte) (name string, ok bool)
}

// EmitCallbacks receives unified output without file headers.
// Callbacks return an error to stop emission.
type EmitCallbacks struct {
	// HunkHeaderFunc replaces the standard hunk header.
	// When nil, LineFunc receives the formatted header as one part.
	// NoHunkHeader suppresses both header paths.
	HunkHeaderFunc func(Hunk) error
	// LineFunc receives a record's prefix, original content, and optional
	// missing-newline notice as separate parts. It also receives default headers.
	// LineFunc is required. The parts and their bytes are borrowed:
	// do not modify them or retain them after the callback returns.
	LineFunc func(parts ...[]byte) error
}

// Line is an output line, including its original newline if present.
type Line struct {
	Operation Operation
	Content   []byte
}

// Hunk contains zero-based line ranges and the corresponding output lines.
type Hunk struct {
	Change
	Function string
	Lines    []Line
}

// MergeLevel selects conflict refinement. The zero value is MergeMinimal.
type MergeLevel uint8

const (
	MergeMinimal MergeLevel = iota
	MergeEager
	MergeZealous
	MergeZealousAlnum
)

// MergeFavor selects automatic conflict resolution.
type MergeFavor uint8

const (
	FavorConflict MergeFavor = iota
	FavorOurs
	FavorTheirs
	FavorUnion
)

// ConflictStyle selects the conflict marker format.
type ConflictStyle uint8

const (
	ConflictMerge ConflictStyle = iota
	ConflictDiff3
	ConflictZDiff3
)

// MergeOptions controls three-way merge. Nil selects MergeZealous and
// seven-character markers. A non-nil options value uses Level as written.
type MergeOptions struct {
	Diff                                  Options
	Level                                 MergeLevel
	Favor                                 MergeFavor
	Style                                 ConflictStyle
	MarkerSize                            int
	AncestorLabel, OursLabel, TheirsLabel string
}

// MergeResult contains merged bytes and the number of unresolved conflicts.
// A conflict is a successful merge computation, not an error.
type MergeResult struct {
	Content   []byte
	Conflicts int
}
