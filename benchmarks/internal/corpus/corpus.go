// Package corpus provides deterministic inputs for the Go and C benchmarks.
package corpus

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

type Case struct {
	Name             string
	Old, New         []byte
	OldText, NewText string
}

type Manifest struct {
	Name      string `json:"name"`
	OldBytes  int    `json:"old_bytes"`
	NewBytes  int    `json:"new_bytes"`
	OldSHA256 string `json:"old_sha256"`
	NewSHA256 string `json:"new_sha256"`
}

func (c Case) Manifest() Manifest {
	oldHash, newHash := sha256.Sum256(c.Old), sha256.Sum256(c.New)
	return Manifest{c.Name, len(c.Old), len(c.New),
		hex.EncodeToString(oldHash[:]), hex.EncodeToString(newHash[:])}
}

func Cases() []Case {
	var cases []Case
	add := func(name string, old, new []byte) {
		cases = append(cases, Case{name, old, new, string(old), string(new)})
	}
	source := func(lines, interval int, unicode bool) ([]byte, []byte) {
		var old, new bytes.Buffer
		for i := 0; i < lines; i++ {
			original := fmt.Sprintf("line %06d: a stable record with some source text\n", i)
			if unicode {
				original = fmt.Sprintf("line %06d: caf\u00e9 \u65e5\u672c\u8a9e \U0001f600\n", i)
			}
			old.WriteString(original)
			if interval > 0 && i%interval == interval/2 {
				fmt.Fprintf(&new, "line %06d: replacement text\n", i)
			} else {
				new.WriteString(original)
			}
		}
		return old.Bytes(), new.Bytes()
	}
	old, new := source(5000, 0, false)
	add("Unchanged", old, new)
	old, new = source(200, 200, false)
	add("SmallEdit", old, new)
	old, new = source(20000, 20000, false)
	add("LargeEdit", old, new)
	old, new = source(5000, 11, false)
	add("Scattered", old, new)
	old = bytes.Repeat([]byte("if condition {\n    repeated()\n}\n\n"), 800)
	new = bytes.Clone(old)
	new = bytes.Replace(new, []byte("    repeated()\n"), []byte("    changed()\n"), 200)
	add("Repeated", old, new)
	var left, right bytes.Buffer
	for i := 0; i < 2000; i++ {
		fmt.Fprintf(&left, "old-only %06d: removed text\n", i)
		fmt.Fprintf(&right, "new-only %06d: inserted text\n", i)
	}
	add("Unrelated", left.Bytes(), right.Bytes())
	old, new = source(2000, 37, true)
	add("Unicode", old, new)
	old = bytes.Repeat(append(bytes.Repeat([]byte("x"), 4096), '\n'), 100)
	new = bytes.Clone(old)
	for i := 0; i < 100; i += 17 {
		new[i*4097+2048] = 'y'
	}
	add("LongLines", old, new)
	return cases
}
