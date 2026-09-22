package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runReport(args []string) {
	cfg := loadConfig()

	sessionDate := ""
	outDir := cfg.Output.Directory
	htmlOutput := false

	for i, a := range args {
		switch a {
		case "--html":
			htmlOutput = true
		case "--out", "-o":
			if i+1 < len(args) {
				outDir = args[i+1]
			}
		default:
			if !strings.HasPrefix(a, "-") && sessionDate == "" {
				sessionDate = a
			}
		}
	}

	sessions, err := discoverSessions(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error discovering sessions: %v\n", err)
		os.Exit(1)
	}

	if len(sessions) == 0 {
		fmt.Fprintln(os.Stderr, "No ISTA sessions found.")
		os.Exit(1)
	}

	var target *Session
	if sessionDate != "" {
		for i := range sessions {
			if sessions[i].Timestamp.Format("2006-01-02") == sessionDate {
				target = &sessions[i]
				break
			}
		}
		if target == nil {
			fmt.Fprintf(os.Stderr, "No session found for date %s\n", sessionDate)
			os.Exit(1)
		}
	} else {
		target = &sessions[0]
	}

	if err := target.ParseMeta(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: meta parse failed: %v\n", err)
	}
	if err := target.ParseTrans(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: trans parse failed: %v\n", err)
	}
	if err := target.ParseZipLog(); err != nil {
		fmt.Fprintf(os.Stderr, "Info: zip.log: %v\n", err)
	}
	if err := target.ParseFASTA(); err != nil {
		fmt.Fprintf(os.Stderr, "Info: FASTA: %v\n", err)
	}

	reportDir := filepath.Join(outDir, target.Timestamp.Format("2006-01-02"))
	os.MkdirAll(reportDir, 0755)

	writeReportData(target, reportDir)

	toolArgs := []string{
		"--data-dir", reportDir,
		"--format", "markdown",
		"--output", filepath.Join(reportDir, "report.md"),
		"--session-date", target.Timestamp.Format("2006-01-02 15:04"),
	}
	if _, err := runTool("report", toolArgs...); err != nil {
		fmt.Fprintf(os.Stderr, "Error generating report: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Report written to: %s\n", filepath.Join(reportDir, "report.md"))

	if htmlOutput {
		screenshotDir := filepath.Join(outDir, target.Timestamp.Format("2006-01-02"))
		htmlArgs := []string{
			"--data-dir", reportDir,
			"--format", "html",
			"--output", filepath.Join(reportDir, "report.html"),
			"--session-date", target.Timestamp.Format("2006-01-02 15:04"),
			"--screenshots-dir", screenshotDir,
		}
		if _, err := runTool("report", htmlArgs...); err != nil {
			fmt.Fprintf(os.Stderr, "Error generating HTML report: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("HTML report written to: %s\n", filepath.Join(reportDir, "report.html"))
	}
}

func writeReportData(s *Session, dir string) {
	vehicle := buildVehicle(s)
	ecus := buildECUs(s)
	faults := buildFaults(s)

	writeJSON(filepath.Join(dir, "vehicle.json"), vehicle)
	writeJSON(filepath.Join(dir, "ecus.json"), ecus)
	writeJSON(filepath.Join(dir, "faults.json"), faults)

	if s.FASTA != nil && len(s.FASTA.Tests) > 0 {
		writeJSON(filepath.Join(dir, "tests.json"), s.FASTA.Tests)
	}
	if s.ZipLogData != nil {
		if s.ZipLogData.ECUKom != nil {
			writeJSON(filepath.Join(dir, "ecukom.json"), s.ZipLogData.ECUKom)
		}
		if len(s.ZipLogData.Timeline) > 0 {
			writeJSON(filepath.Join(dir, "timeline.json"), s.ZipLogData.Timeline)
		}
	}
}
