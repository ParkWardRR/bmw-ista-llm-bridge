package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

type Encoder struct {
	Codec string
	Ext   string
	Accel string
	Args  []string
}

func probeEncoder(logger *slog.Logger, quality, preset int) (*Encoder, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not found in PATH: %w", err)
	}

	candidates := []Encoder{
		{
			Codec: "av1_qsv",
			Ext:   "avif",
			Accel: "Intel QSV",
			Args: []string{
				"-c:v", "av1_qsv",
				"-global_quality", fmt.Sprint(quality),
				"-pix_fmt", "nv12",
			},
		},
		{
			Codec: "av1_nvenc",
			Ext:   "avif",
			Accel: "NVIDIA NVENC",
			Args: []string{
				"-c:v", "av1_nvenc",
				"-cq", fmt.Sprint(quality),
				"-preset", "p4",
				"-pix_fmt", "p010le",
			},
		},
		{
			Codec: "av1_amf",
			Ext:   "avif",
			Accel: "AMD AMF",
			Args: []string{
				"-c:v", "av1_amf",
				"-quality", fmt.Sprint(quality),
				"-pix_fmt", "nv12",
			},
		},
		{
			Codec: "libaom-av1",
			Ext:   "avif",
			Accel: "CPU",
			Args: []string{
				"-c:v", "libaom-av1",
				"-crf", fmt.Sprint(quality),
				"-cpu-used", fmt.Sprint(preset),
				"-still-picture", "1",
				"-pix_fmt", "yuv444p10le",
				"-b:v", "0",
			},
		},
		{
			Codec: "libsvtav1",
			Ext:   "avif",
			Accel: "CPU",
			Args: []string{
				"-c:v", "libsvtav1",
				"-crf", fmt.Sprint(quality),
				"-preset", fmt.Sprint(preset),
				"-svtav1-params", "still-picture=1",
				"-pix_fmt", "yuv420p10le",
			},
		},
		{
			Codec: "librav1e",
			Ext:   "avif",
			Accel: "CPU",
			Args: []string{
				"-c:v", "librav1e",
				"-qp", fmt.Sprint(quality + 40),
				"-speed", fmt.Sprint(preset),
				"-pix_fmt", "yuv444p",
			},
		},
		{
			Codec: "libwebp",
			Ext:   "webp",
			Accel: "CPU",
			Args: []string{
				"-c:v", "libwebp",
				"-quality", fmt.Sprint(95 - quality),
				"-preset", "text",
			},
		},
	}

	tmpDir := os.TempDir()
	for _, enc := range candidates {
		out := filepath.Join(tmpDir, "ista_bridge_probe."+enc.Ext)
		args := []string{
			"-hide_banner", "-loglevel", "error",
			"-f", "lavfi", "-i", "color=c=red:s=64x64:d=1",
			"-frames:v", "1",
		}
		args = append(args, enc.Args...)
		args = append(args, "-y", out)

		cmd := exec.Command("ffmpeg", args...)
		if err := cmd.Run(); err != nil {
			logger.Debug("encoder unavailable", "codec", enc.Codec, "accel", enc.Accel)
			continue
		}

		fi, err := os.Stat(out)
		os.Remove(out)
		if err != nil || fi.Size() == 0 {
			logger.Debug("encoder produced empty output", "codec", enc.Codec)
			continue
		}

		logger.Debug("encoder available", "codec", enc.Codec, "accel", enc.Accel, "probe_bytes", fi.Size())
		return &enc, nil
	}

	return nil, fmt.Errorf("all encoders failed — is ffmpeg installed?")
}

func encode(enc *Encoder, pixels []byte, w, h int, outPath string) error {
	args := []string{
		"-hide_banner", "-loglevel", "error",
		"-f", "rawvideo",
		"-pix_fmt", "bgra",
		"-s", fmt.Sprintf("%dx%d", w, h),
		"-i", "pipe:0",
		"-frames:v", "1",
	}
	args = append(args, enc.Args...)
	args = append(args, "-y", outPath)

	cmd := exec.Command("ffmpeg", args...)
	cmd.Stdin = bytes.NewReader(pixels)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg %s: %w\n%s", enc.Codec, err, output)
	}
	return nil
}
