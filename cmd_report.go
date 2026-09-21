package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func runReport(args []string) {
	cfg := loadConfig()

	sessionDate := ""
	outDir := cfg.Output.Directory
	jsonOutput := false

	for i, a := range args {
		switch a {
		case "--json", "-j":
			jsonOutput = true
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

	// Find target session
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

	// Generate report
	report := generateReport(target, jsonOutput)

	// Write output
	reportDir := filepath.Join(outDir, target.Timestamp.Format("2006-01-02"))
	os.MkdirAll(reportDir, 0755)

	reportPath := filepath.Join(reportDir, "report.md")
	if err := os.WriteFile(reportPath, []byte(report), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "Error writing report: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Report written to: %s\n", reportPath)
}

func generateReport(s *Session, jsonMode bool) string {
	var b strings.Builder

	b.WriteString("# ISTA Diagnostic Report\n\n")
	b.WriteString(fmt.Sprintf("**Generated:** %s\n", time.Now().Format("2006-01-02 15:04:05")))
	b.WriteString(fmt.Sprintf("**Session:** %s\n\n", s.Timestamp.Format("2006-01-02 15:04")))

	// Vehicle Info
	b.WriteString("## Vehicle Information\n\n")
	if s.Meta != nil {
		b.WriteString(fmt.Sprintf("- **VIN:** %s\n", s.Meta.VIN17))
		b.WriteString(fmt.Sprintf("- **Brand:** %s\n", s.Meta.Features.Brand))
		b.WriteString(fmt.Sprintf("- **Series:** %s\n", s.Meta.Features.Series))
		b.WriteString(fmt.Sprintf("- **Model Series:** %s\n", s.Meta.Features.ModelSeries))
		b.WriteString(fmt.Sprintf("- **Body:** %s\n", s.Meta.Features.Body))
		b.WriteString(fmt.Sprintf("- **Engine:** %s\n", s.Meta.Features.Engine))
		b.WriteString(fmt.Sprintf("- **Transmission:** %s\n", s.Meta.Features.Transmission))
		b.WriteString(fmt.Sprintf("- **Model Year:** %s\n", s.Meta.Features.ModelYear))
		b.WriteString(fmt.Sprintf("- **Market:** %s\n", s.Meta.Features.Market))
		b.WriteString(fmt.Sprintf("- **Mileage:** %d km\n", s.Meta.Mileage))
		b.WriteString(fmt.Sprintf("- **Communication:** %s\n", s.Meta.CommType))
		b.WriteString(fmt.Sprintf("- **Dealer:** %s\n", s.Meta.Dealer))
		b.WriteString(fmt.Sprintf("- **Session State:** %s\n", s.Meta.State))
	}

	if s.Trans != nil {
		b.WriteString(fmt.Sprintf("- **I-Level:** %s\n", s.Trans.ILevel))
		b.WriteString(fmt.Sprintf("- **Mileage (trans):** %d %s\n", s.Trans.Mileage, s.Trans.Unit))
	}

	// Fault Codes
	b.WriteString("\n## Fault Codes\n\n")
	faults := s.AllFaults()
	if len(faults) == 0 {
		b.WriteString("No fault codes found.\n")
	} else {
		b.WriteString(fmt.Sprintf("Found %d fault code(s):\n\n", len(faults)))
		b.WriteString("| ECU | Bus | Code | Description | Status | Warning |\n")
		b.WriteString("|-----|-----|------|-------------|--------|--------|\n")
		for _, f := range faults {
			pcode := f.DTC.PCode
			if pcode == "" {
				pcode = f.DTC.SAECode
			}
			if pcode == "" {
				pcode = f.DTC.HexCode
			}
			desc := f.DTC.Description
			if len(desc) > 60 {
				desc = desc[:57] + "..."
			}
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s |\n",
				f.ECUName, f.Bus, pcode, desc, f.DTC.StatusText, f.DTC.WarningText))
		}
	}

	// ECU List
	b.WriteString("\n## ECU List\n\n")
	if s.Trans != nil && len(s.Trans.ECUs) > 0 {
		b.WriteString(fmt.Sprintf("Found %d ECU(s):\n\n", len(s.Trans.ECUs)))
		b.WriteString("| ECU | Full Name | Bus | SGBD | Protocol | Status |\n")
		b.WriteString("|-----|-----------|-----|------|----------|--------|\n")
		for _, ecu := range s.Trans.ECUs {
			status := ecu.CommSuccess
			if status == "" {
				status = "—"
			}
			name := ecu.TreeName
			if name == "" {
				name = ecu.ShortName
			}
			b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s |\n",
				name, ecu.FullName, ecu.Bus, ecu.SGBD, ecu.Protocol, status))
		}
	} else {
		b.WriteString("No ECU data available.\n")
	}

	// Software Versions
	if s.Trans != nil {
		b.WriteString("\n## Software Versions (I-Level)\n\n")
		b.WriteString(fmt.Sprintf("- **I-Level:** %s\n", s.Trans.ILevel))
		if len(s.Trans.ECUs) > 0 {
			for _, ecu := range s.Trans.ECUs {
				if len(ecu.SVK.SGBMIDs) > 0 {
					b.WriteString(fmt.Sprintf("\n### %s\n", ecu.TreeName))
					for _, id := range ecu.SVK.SGBMIDs {
						b.WriteString(fmt.Sprintf("- %s\n", id))
					}
					if ecu.SVK.ProgDate != "" {
						b.WriteString(fmt.Sprintf("- Last programmed: %s\n", ecu.SVK.ProgDate))
					}
				}
			}
		}
	}

	// Session Files
	b.WriteString("\n## Session Files\n\n")
	if s.TransFile != "" {
		b.WriteString(fmt.Sprintf("- Transaction: `%s`\n", s.TransFile))
	}
	if s.MetaFile != "" {
		b.WriteString(fmt.Sprintf("- Metadata: `%s`\n", s.MetaFile))
	}
	if s.ZipLog != "" {
		b.WriteString(fmt.Sprintf("- Log bundle: `%s`\n", s.ZipLog))
	}
	if s.BehDat != "" {
		b.WriteString(fmt.Sprintf("- Behavioral data: `%s`\n", s.BehDat))
	}
	if s.FstDat != "" {
		b.WriteString(fmt.Sprintf("- Test data: `%s`\n", s.FstDat))
	}

	b.WriteString("\n---\n")
	b.WriteString("*Generated by ista-bridge*\n")

	return b.String()
}
