package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"time"
	"unsafe"

	"github.com/corona10/goimagehash"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "bundle":
			runBundle(os.Args[2:])
			return
		case "sessions":
			runSessions()
			return
		case "watch":
			os.Args = append(os.Args[:1], os.Args[2:]...)
			runWatch()
			return
		case "db":
			runDB(os.Args[2:])
			return
		case "vin":
			runVINLookup(os.Args[2:])
			return
		case "lookup":
			runLookup(os.Args[2:])
			return
		case "report":
			runReport(os.Args[2:])
			return
		case "odincs":
			runOdincs(os.Args[2:])
			return
		}
	}
	runTUI()
}

func runSessions() {
	cfg := loadConfig()
	sessions, err := discoverSessions(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(sessions) == 0 {
		fmt.Println("No ISTA sessions found.")
		fmt.Printf("Looked in: %s\n", filepath.Join(cfg.ISTA.InstallDir, "Transactions"))
		return
	}
	fmt.Printf("Found %d session(s):\n\n", len(sessions))
	for i, s := range sessions {
		fmt.Printf("  %d. %s  VIN: %s", i+1, s.Timestamp.Format("2006-01-02 15:04"), s.VIN)
		if s.Model != "" {
			fmt.Printf("  Model: %s", s.Model)
		}
		fmt.Println()
		if s.TransFile != "" {
			fmt.Println("     ✓ Transaction data")
		}
		if s.ZipLog != "" {
			fmt.Println("     ✓ Log bundle (.zip.log)")
		}
		if s.BehDat != "" {
			fmt.Println("     ✓ FASTA behavioral data")
		}
		if s.FstDat != "" {
			fmt.Println("     ✓ FASTA test data")
		}
	}
}

func runBundle(args []string) {
	cfg := loadConfig()
	logger := setupLogging(cfg.Logging)

	fs := flag.NewFlagSet("bundle", flag.ExitOnError)
	outDir := fs.String("out", cfg.Output.Directory, "Output directory for bundle")
	sessionDate := fs.String("session", "", "Session date to bundle (YYYY-MM-DD); defaults to latest")
	fs.Parse(args)

	sessions, err := discoverSessions(cfg)
	if err != nil {
		logger.Error("discover sessions failed", "error", err)
		os.Exit(1)
	}
	if len(sessions) == 0 {
		fmt.Println("No ISTA sessions found.")
		os.Exit(1)
	}

	var target *Session
	if *sessionDate != "" {
		for i := range sessions {
			if sessions[i].Timestamp.Format("2006-01-02") == *sessionDate {
				target = &sessions[i]
				break
			}
		}
		if target == nil {
			fmt.Fprintf(os.Stderr, "No session found for date %s\n", *sessionDate)
			os.Exit(1)
		}
	} else {
		target = &sessions[0]
	}

	if err := target.ParseMeta(); err != nil {
		logger.Warn("meta parse failed (continuing)", "error", err)
	}
	if err := target.ParseTrans(); err != nil {
		logger.Warn("trans parse failed (continuing)", "error", err)
	}
	if err := target.ParseZipLog(); err != nil {
		logger.Info("zip.log parse skipped", "reason", err)
	}
	if err := target.ParseFASTA(); err != nil {
		logger.Info("FASTA parse skipped", "reason", err)
	}

	if err := bundleSession(logger, target, *outDir); err != nil {
		logger.Error("bundle failed", "error", err)
		os.Exit(1)
	}
}

func runWatch() {
	cfg := loadConfig()

	outDir := flag.String("out", cfg.Output.Directory, "Output directory")
	poll := flag.Duration("poll", time.Duration(cfg.Capture.PollMs)*time.Millisecond, "Polling interval for change detection")
	debounce := flag.Duration("debounce", time.Duration(cfg.Capture.DebounceMs)*time.Millisecond, "Settle time after change before capturing")
	title := flag.String("window", cfg.Window.Title, "Window title substring to match")
	threshold := flag.Float64("threshold", cfg.Capture.Threshold, "Min pixel change fraction to trigger (0.005 = 0.5%)")
	quality := flag.Int("quality", cfg.Encoding.Quality, "Encoder quality (lower = higher quality; CRF for AVIF)")
	preset := flag.Int("preset", cfg.Encoding.Preset, "Encoder speed (libaom: 0-6, svtav1: 0-12; higher=faster)")
	flag.Parse()

	cfg.Window.Title = *title
	cfg.Capture.PollMs = int(poll.Milliseconds())
	cfg.Capture.DebounceMs = int(debounce.Milliseconds())
	cfg.Capture.Threshold = *threshold
	cfg.Encoding.Quality = *quality
	cfg.Encoding.Preset = *preset
	cfg.Output.Directory = *outDir

	logger := setupLogging(cfg.Logging)

	sessionDir := cfg.Output.Directory
	if cfg.Output.SessionFolders {
		sessionDir = filepath.Join(cfg.Output.Directory, time.Now().Format("2006-01-02"))
	}
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		logger.Error("failed to create output directory", "path", sessionDir, "error", err)
		os.Exit(1)
	}

	setDPIAware()

	hwnd, winTitle, err := findWindowByTitle(logger, cfg.Window.Title)
	if err != nil {
		logger.Error("window search failed", "error", err)
		os.Exit(1)
	}
	logger.Info("target window found", "title", winTitle)

	enc, err := probeEncoder(logger, cfg.Encoding.Quality, cfg.Encoding.Preset)
	if err != nil {
		logger.Error("encoder probe failed", "error", err)
		os.Exit(1)
	}
	logger.Info("encoder selected", "codec", enc.Codec, "ext", enc.Ext)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)

	logger.Info("monitoring started",
		"poll", poll.String(),
		"debounce", debounce.String(),
		"threshold", fmt.Sprintf("%.1f%%", *threshold*100),
		"phash_threshold", cfg.Capture.PHashThreshold,
		"output", sessionDir,
	)

	var prev []byte
	var prevW, prevH int
	var prevHash *goimagehash.ImageHash
	count := 0
	var totalBytes int64
	var pHashSkips int64
	startTime := time.Now()
	pollDur := time.Duration(cfg.Capture.PollMs) * time.Millisecond
	debounceDur := time.Duration(cfg.Capture.DebounceMs) * time.Millisecond

	ticker := time.NewTicker(pollDur)
	defer ticker.Stop()

	for {
		select {
		case <-sigCh:
			logger.Info("session ended",
				"screenshots", count,
				"total_size", fmt.Sprintf("%.1fKB", float64(totalBytes)/1024),
				"phash_skips", pHashSkips,
				"duration", time.Since(startTime).Round(time.Second).String(),
				"output", sessionDir,
			)
			return
		case <-ticker.C:
			if !isWindowValid(hwnd) {
				logger.Info("window closed")
				return
			}

			pixels, w, h, err := captureWindow(hwnd)
			if err != nil {
				continue
			}

			if prev != nil && w == prevW && h == prevH {
				if cfg.Capture.PHashThreshold > 0 && prevHash != nil {
					hash, err := computePHash(pixels, w, h)
					if err == nil {
						dist, _ := hash.Distance(prevHash)
						if dist <= cfg.Capture.PHashThreshold {
							pHashSkips++
							continue
						}
					}
				}

				ratio := diffRatio(prev, pixels)
				if ratio < cfg.Capture.Threshold {
					continue
				}
				logger.Debug("change detected", "diff", fmt.Sprintf("%.2f%%", ratio*100))
				time.Sleep(debounceDur)

				pixels, w, h, err = captureWindow(hwnd)
				if err != nil {
					continue
				}
			}

			prev = pixels
			prevW = w
			prevH = h
			prevHash, _ = computePHash(pixels, w, h)

			ts := time.Now().Format("15-04-05.000")
			outPath := filepath.Join(sessionDir, fmt.Sprintf("ista_%s.%s", ts, enc.Ext))

			if err := encode(enc, pixels, w, h, outPath); err != nil {
				logger.Error("encode failed", "error", err)
				continue
			}

			fi, _ := os.Stat(outPath)
			size := fi.Size()
			count++
			totalBytes += size
			logger.Info("captured",
				"n", count,
				"file", filepath.Base(outPath),
				"size", fmt.Sprintf("%.1fKB", float64(size)/1024),
				"res", fmt.Sprintf("%dx%d", w, h),
			)
		}
	}
}

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
