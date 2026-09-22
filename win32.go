//go:build windows

package main

import (
	"fmt"
	"log/slog"
	"strings"
	"syscall"
	"unsafe"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	pEnumWindows          = user32.NewProc("EnumWindows")
	pGetWindowTextW       = user32.NewProc("GetWindowTextW")
	pGetWindowTextLengthW = user32.NewProc("GetWindowTextLengthW")
	pGetClientRect        = user32.NewProc("GetClientRect")
	pGetDC                = user32.NewProc("GetDC")
	pReleaseDC            = user32.NewProc("ReleaseDC")
	pPrintWindow          = user32.NewProc("PrintWindow")
	pIsWindow             = user32.NewProc("IsWindow")
	pSetProcessDPIAware   = user32.NewProc("SetProcessDPIAware")

	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pGetDIBits              = gdi32.NewProc("GetDIBits")
)

const (
	srccopy             = 0x00CC0020
	dibRGBColors        = 0
	biRGB               = 0
	pwRenderfullcontent = 0x00000002
)

type rect struct {
	Left, Top, Right, Bottom int32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func setDPIAware() {
	pSetProcessDPIAware.Call()
}

func isWindowValid(hwnd syscall.Handle) bool {
	ret, _, _ := pIsWindow.Call(uintptr(hwnd))
	return ret != 0
}

type windowMatch struct {
	hwnd  syscall.Handle
	title string
	score int // higher = better match
}

func findWindowByTitle(logger *slog.Logger, substr string) (syscall.Handle, string, error) {
	var found []windowMatch
	lower := strings.ToLower(substr)

	cb := syscall.NewCallback(func(hwnd syscall.Handle, _ uintptr) uintptr {
		tLen, _, _ := pGetWindowTextLengthW.Call(uintptr(hwnd))
		if tLen == 0 {
			return 1
		}
		buf := make([]uint16, tLen+1)
		pGetWindowTextW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), tLen+1)
		title := syscall.UTF16ToString(buf)
		titleLow := strings.ToLower(title)

		if !strings.Contains(titleLow, lower) {
			return 1
		}

		score := 1
		if strings.EqualFold(title, substr) {
			score = 100 // exact match
		} else if strings.HasPrefix(titleLow, lower+" ") || strings.HasPrefix(titleLow, lower+"-") ||
			strings.HasPrefix(titleLow, lower+":") || titleLow == lower {
			score = 50 // starts with search term as a word
		} else {
			idx := strings.Index(titleLow, lower)
			if idx > 0 {
				prev := titleLow[idx-1]
				if prev == ' ' || prev == '-' || prev == '(' || prev == '[' {
					score = 30 // word boundary before match
				}
			}
		}
		found = append(found, windowMatch{hwnd, title, score})
		return 1
	})

	pEnumWindows.Call(cb, 0)

	if len(found) == 0 {
		return 0, "", fmt.Errorf("no window with title containing %q", substr)
	}

	// Sort by score descending
	for i := 0; i < len(found)-1; i++ {
		for j := i + 1; j < len(found); j++ {
			if found[j].score > found[i].score {
				found[i], found[j] = found[j], found[i]
			}
		}
	}

	if len(found) > 1 {
		for i, m := range found {
			logger.Debug("window candidate",
				"rank", i,
				"title", m.title,
				"score", m.score,
				"selected", i == 0,
			)
		}
	}
	return found[0].hwnd, found[0].title, nil
}

func captureWindow(hwnd syscall.Handle) ([]byte, int, int, error) {
	var r rect
	pGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	w := int(r.Right - r.Left)
	h := int(r.Bottom - r.Top)
	if w <= 0 || h <= 0 {
		return nil, 0, 0, fmt.Errorf("invalid window size %dx%d (minimized?)", w, h)
	}

	hdcWin, _, _ := pGetDC.Call(uintptr(hwnd))
	if hdcWin == 0 {
		return nil, 0, 0, fmt.Errorf("GetDC failed")
	}
	defer pReleaseDC.Call(uintptr(hwnd), hdcWin)

	hdcMem, _, _ := pCreateCompatibleDC.Call(hdcWin)
	if hdcMem == 0 {
		return nil, 0, 0, fmt.Errorf("CreateCompatibleDC failed")
	}
	defer pDeleteDC.Call(hdcMem)

	hBmp, _, _ := pCreateCompatibleBitmap.Call(hdcWin, uintptr(w), uintptr(h))
	if hBmp == 0 {
		return nil, 0, 0, fmt.Errorf("CreateCompatibleBitmap failed")
	}
	defer pDeleteObject.Call(hBmp)

	old, _, _ := pSelectObject.Call(hdcMem, hBmp)
	defer pSelectObject.Call(hdcMem, old)

	// PrintWindow with PW_RENDERFULLCONTENT works for DX/WPF/Chromium apps
	ret, _, _ := pPrintWindow.Call(uintptr(hwnd), hdcMem, pwRenderfullcontent)
	if ret == 0 {
		// Fallback: BitBlt from client DC
		pBitBlt.Call(hdcMem, 0, 0, uintptr(w), uintptr(h), hdcWin, 0, 0, srccopy)
	}

	bmi := bitmapInfoHeader{
		Size:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
		Width:       int32(w),
		Height:      -int32(h), // negative = top-down row order
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}

	pixels := make([]byte, w*h*4)
	got, _, _ := pGetDIBits.Call(
		hdcMem, hBmp,
		0, uintptr(h),
		uintptr(unsafe.Pointer(&pixels[0])),
		uintptr(unsafe.Pointer(&bmi)),
		dibRGBColors,
	)
	if got == 0 {
		return nil, 0, 0, fmt.Errorf("GetDIBits failed")
	}

	// Quick sanity: if first 4096 bytes are all zero, PrintWindow gave us a black frame
	if ret != 0 {
		allBlack := true
		end := 4096
		if end > len(pixels) {
			end = len(pixels)
		}
		for i := 0; i < end; i++ {
			if pixels[i] != 0 {
				allBlack = false
				break
			}
		}
		if allBlack {
			// Retry with BitBlt
			pBitBlt.Call(hdcMem, 0, 0, uintptr(w), uintptr(h), hdcWin, 0, 0, srccopy)
			pGetDIBits.Call(
				hdcMem, hBmp,
				0, uintptr(h),
				uintptr(unsafe.Pointer(&pixels[0])),
				uintptr(unsafe.Pointer(&bmi)),
				dibRGBColors,
			)
		}
	}

	return pixels, w, h, nil
}
