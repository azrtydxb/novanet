// Package service implements internal services for the NovaNet controller,
// including load-balancing algorithms and backend allocation logic.
package service

import "hash/fnv"

// GenerateMaglevTable generates a Maglev consistent hash lookup table.
// Each entry maps to a backend index (0-based).
func GenerateMaglevTable(backends []string, tableSize int) []uint32 {
	n := len(backends)
	if n == 0 {
		return make([]uint32, tableSize)
	}

	if tableSize <= 0 || tableSize > int(^uint32(0)) {
		return nil
	}
	ts64 := uint64(tableSize)
	ts32 := uint32(tableSize)

	offsets := make([]uint32, n)
	skips := make([]uint32, n)
	for i, b := range backends {
		h := fnv.New64a()
		_, _ = h.Write([]byte(b))
		hash := h.Sum64()
		offsets[i] = uint32(hash % ts64)         //nolint:gosec // result < ts64 which fits in uint32
		skips[i] = uint32(hash>>32%(ts64-1)) + 1 //nolint:gosec // result < ts64-1 which fits in uint32
	}

	table := make([]uint32, tableSize)
	for i := range table {
		table[i] = ^uint32(0) // sentinel "empty"
	}

	next := make([]uint32, n)
	for i := range next {
		next[i] = offsets[i]
	}

	filled := 0
	for filled < tableSize {
		for i := range n {
			pos := next[i]
			for table[pos] != ^uint32(0) {
				pos = (pos + skips[i]) % ts32
			}
			table[pos] = uint32(i)
			next[i] = (pos + skips[i]) % ts32
			filled++
			if filled >= tableSize {
				break
			}
		}
	}

	return table
}
