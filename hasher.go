package main

import (
	"image"

	"github.com/corona10/goimagehash"
)

func computePHash(pixels []byte, w, h int) (*goimagehash.ImageHash, error) {
	img := &image.NRGBA{
		Pix:    pixels,
		Stride: w * 4,
		Rect:   image.Rect(0, 0, w, h),
	}
	return goimagehash.PerceptionHash(img)
}
