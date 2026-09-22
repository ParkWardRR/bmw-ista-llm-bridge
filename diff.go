package main

import "unsafe"

// diffRatio compares two BGRA pixel buffers and returns the fraction of changed pixels.
// Uses 64-bit block comparison: XORs two pixels at a time via uint64, masking out alpha.
// On amd64/arm64 this compiles to native 64-bit ops; the Go compiler can auto-vectorize
// the tight loop when AVX2 or NEON is available.
func diffRatio(a, b []byte) float64 {
	if len(a) != len(b) {
		return 1.0
	}
	n := len(a)
	total := n / 4

	diff := 0
	const rgbMask = 0x00FFFFFF00FFFFFF
	blocks := n / 8
	for i := 0; i < blocks; i++ {
		off := i * 8
		va := *(*uint64)(unsafe.Pointer(&a[off]))
		vb := *(*uint64)(unsafe.Pointer(&b[off]))
		xor := (va ^ vb) & rgbMask
		if xor != 0 {
			if xor&0x00FFFFFF != 0 {
				diff++
			}
			if xor&0x00FFFFFF00000000 != 0 {
				diff++
			}
		}
	}
	for i := blocks * 8; i+3 < n; i += 4 {
		if a[i] != b[i] || a[i+1] != b[i+1] || a[i+2] != b[i+2] {
			diff++
		}
	}
	return float64(diff) / float64(total)
}
