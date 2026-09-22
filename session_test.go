package main

import (
	"math"
	"testing"
)

func TestDiffRatio(t *testing.T) {
	t.Run("identical buffers return 0", func(t *testing.T) {
		// 4 pixels, BGRA format (4 bytes per pixel)
		buf := make([]byte, 16)
		for i := range buf {
			buf[i] = 0x80
		}
		a := make([]byte, len(buf))
		copy(a, buf)

		got := diffRatio(a, buf)
		if got != 0.0 {
			t.Errorf("diffRatio(identical) = %f, want 0.0", got)
		}
	})

	t.Run("completely different buffers return 1", func(t *testing.T) {
		// 4 pixels, every RGB byte different (ignore alpha at byte 3,7,11,15)
		a := make([]byte, 16)
		b := make([]byte, 16)
		for i := 0; i < 16; i += 4 {
			a[i] = 0x00   // B
			a[i+1] = 0x00 // G
			a[i+2] = 0x00 // R
			a[i+3] = 0xFF // A

			b[i] = 0xFF   // B
			b[i+1] = 0xFF // G
			b[i+2] = 0xFF // R
			b[i+3] = 0xFF // A
		}

		got := diffRatio(a, b)
		if got != 1.0 {
			t.Errorf("diffRatio(all different) = %f, want 1.0", got)
		}
	})

	t.Run("different length buffers return 1", func(t *testing.T) {
		a := make([]byte, 16)
		b := make([]byte, 20)

		got := diffRatio(a, b)
		if got != 1.0 {
			t.Errorf("diffRatio(different lengths) = %f, want 1.0", got)
		}
	})

	t.Run("single pixel change in four pixel buffer", func(t *testing.T) {
		// 4 pixels (16 bytes). Change one pixel's RGB.
		a := make([]byte, 16)
		b := make([]byte, 16)
		// Make all pixels identical
		for i := range a {
			a[i] = 0x40
			b[i] = 0x40
		}
		// Change pixel 0's blue channel
		b[0] = 0xFF

		got := diffRatio(a, b)
		want := 1.0 / 4.0 // 1 pixel changed out of 4
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("diffRatio(1 of 4 changed) = %f, want %f", got, want)
		}
	})

	t.Run("alpha only change is ignored", func(t *testing.T) {
		// BGRA: only alpha (byte index 3) differs
		a := []byte{0x10, 0x20, 0x30, 0x00, 0x10, 0x20, 0x30, 0x00}
		b := []byte{0x10, 0x20, 0x30, 0xFF, 0x10, 0x20, 0x30, 0xFF}

		got := diffRatio(a, b)
		if got != 0.0 {
			t.Errorf("diffRatio(alpha only) = %f, want 0.0", got)
		}
	})

	t.Run("empty buffers", func(t *testing.T) {
		a := []byte{}
		b := []byte{}
		// len(a) == len(b) == 0, total = 0/4 = 0 pixels, diff = 0
		// This would divide by zero; check it does not panic
		got := diffRatio(a, b)
		// 0/0 in float64 is NaN; accept NaN or 0
		if !math.IsNaN(got) && got != 0.0 {
			t.Errorf("diffRatio(empty) = %f, want NaN or 0.0", got)
		}
	})

	t.Run("half pixels changed", func(t *testing.T) {
		// 8 pixels (32 bytes). Change 4 of them.
		a := make([]byte, 32)
		b := make([]byte, 32)
		for i := range a {
			a[i] = 0x50
			b[i] = 0x50
		}
		// Change pixels 0,1,2,3 (first 16 bytes, RGB channels only)
		for i := 0; i < 16; i += 4 {
			b[i] = 0xFF   // B
			b[i+1] = 0xFF // G
			b[i+2] = 0xFF // R
			// leave alpha unchanged
		}

		got := diffRatio(a, b)
		want := 4.0 / 8.0 // 4 pixels changed out of 8
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("diffRatio(half changed) = %f, want %f", got, want)
		}
	})

	t.Run("odd pixel count handles tail", func(t *testing.T) {
		// 3 pixels = 12 bytes (not a multiple of 8, so the tail loop runs)
		a := make([]byte, 12)
		b := make([]byte, 12)
		for i := range a {
			a[i] = 0x30
			b[i] = 0x30
		}
		// Change pixel 2 (bytes 8,9,10) which is in the tail
		b[8] = 0xFF

		got := diffRatio(a, b)
		want := 1.0 / 3.0
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("diffRatio(tail pixel change) = %f, want %f", got, want)
		}
	})
}
