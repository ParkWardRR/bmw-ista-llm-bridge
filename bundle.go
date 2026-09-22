package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type BundleVehicle struct {
	VIN          string `json:"vin"`
	Brand        string `json:"brand"`
	Series       string `json:"series"`
	ModelSeries  string `json:"model_series"`
	Body         string `json:"body"`
	Engine       string `json:"engine"`
	Transmission string `json:"transmission"`
	ModelYear    string `json:"model_year"`
	Market       string `json:"market"`
	Steering     string `json:"steering"`
	Assembly     string `json:"assembly_country"`
	ILevel       string `json:"i_level"`
	Mileage      int    `json:"mileage_km"`
	CommType     string `json:"comm_type"`
}

type BundleFault struct {
	ECU         string `json:"ecu"`
	ECUFullName string `json:"ecu_full_name"`
	Bus         string `json:"bus"`
	Code        int    `json:"code"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Warning     string `json:"warning"`
	PCode       string `json:"p_code,omitempty"`
	MileageKM   int    `json:"mileage_km,omitempty"`
}

type BundleECU struct {
	Name        string   `json:"name"`
	FullName    string   `json:"full_name"`
	Variant     string   `json:"variant"`
	Bus         string   `json:"bus"`
	Protocol    string   `json:"protocol"`
	Supplier    string   `json:"supplier"`
	FaultCount  int      `json:"fault_count"`
	CommOK      bool     `json:"comm_ok"`
	SoftwareIDs []string `json:"software_ids,omitempty"`
}

func bundleSession(logger *slog.Logger, s *Session, outDir string) error {
	ts := s.Timestamp.Format("2006-01-02_150405")
	bundleDir := filepath.Join(outDir, fmt.Sprintf("session_%s_%s", ts, s.VIN))
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		return fmt.Errorf("create bundle dir: %w", err)
	}

	vehicle := buildVehicle(s)
	ecus := buildECUs(s)
	faults := buildFaults(s)

	if err := writeJSON(filepath.Join(bundleDir, "vehicle.json"), vehicle); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(bundleDir, "ecus.json"), ecus); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(bundleDir, "faults.json"), faults); err != nil {
		return err
	}

	var ecukomJobs int
	if s.ZipLogData != nil && s.ZipLogData.ECUKom != nil {
		if err := writeJSON(filepath.Join(bundleDir, "ecukom.json"), s.ZipLogData.ECUKom); err != nil {
			logger.Warn("ecukom.json write failed", "error", err)
		} else {
			ecukomJobs = len(s.ZipLogData.ECUKom.Jobs)
		}
	}

	var timelineEvents int
	if s.ZipLogData != nil && len(s.ZipLogData.Timeline) > 0 {
		if err := writeJSON(filepath.Join(bundleDir, "timeline.json"), s.ZipLogData.Timeline); err != nil {
			logger.Warn("timeline.json write failed", "error", err)
		} else {
			timelineEvents = len(s.ZipLogData.Timeline)
		}
	}

	var testCount int
	if s.FASTA != nil && len(s.FASTA.Tests) > 0 {
		if err := writeJSON(filepath.Join(bundleDir, "tests.json"), s.FASTA.Tests); err != nil {
			logger.Warn("tests.json write failed", "error", err)
		} else {
			testCount = len(s.FASTA.Tests)
		}
	}

	if err := writeSummary(bundleDir, s); err != nil {
		return err
	}

	screenshotCount := copyScreenshots(logger, outDir, bundleDir, s.Timestamp)

	logger.Info("bundle created",
		"path", bundleDir,
		"ecus", len(ecus),
		"faults", len(faults),
		"ecukom_jobs", ecukomJobs,
		"timeline_events", timelineEvents,
		"tests", testCount,
	)
	fmt.Printf("Bundle: %s\n", bundleDir)
	fmt.Printf("  Vehicle: %s %s %s (%s) — %d km\n", vehicle.ModelYear, vehicle.Brand, vehicle.ModelSeries, vehicle.Engine, vehicle.Mileage)
	fmt.Printf("  I-Level: %s\n", vehicle.ILevel)
	fmt.Printf("  ECUs: %d\n", len(ecus))
	fmt.Printf("  Faults: %d\n", len(faults))
	if ecukomJobs > 0 {
		fmt.Printf("  EDIABAS jobs: %d\n", ecukomJobs)
	}
	if timelineEvents > 0 {
		fmt.Printf("  Timeline events: %d\n", timelineEvents)
	}
	if testCount > 0 {
		fmt.Printf("  FASTA tests: %d\n", testCount)
	}
	if screenshotCount > 0 {
		fmt.Printf("  Screenshots: %d\n", screenshotCount)
	}

	files := "vehicle.json, ecus.json, faults.json, summary.md"
	if ecukomJobs > 0 {
		files += ", ecukom.json"
	}
	if timelineEvents > 0 {
		files += ", timeline.json"
	}
	if testCount > 0 {
		files += ", tests.json"
	}
	fmt.Printf("  Files: %s\n", files)

	return nil
}

func copyScreenshots(logger *slog.Logger, outDir, bundleDir string, ts time.Time) int {
	screenshotDir := filepath.Join(outDir, ts.Format("2006-01-02"))
	info, err := os.Stat(screenshotDir)
	if err != nil || !info.IsDir() {
		return 0
	}

	dstScreenshots := filepath.Join(bundleDir, "screenshots")
	if err := os.MkdirAll(dstScreenshots, 0755); err != nil {
		return 0
	}

	entries, _ := os.ReadDir(screenshotDir)
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		src := filepath.Join(screenshotDir, e.Name())
		dst := filepath.Join(dstScreenshots, e.Name())
		data, err := os.ReadFile(src)
		if err == nil {
			os.WriteFile(dst, data, 0644)
			count++
		}
	}
	if count > 0 {
		logger.Info("screenshots copied", "count", count, "from", screenshotDir)
	}
	return count
}

func buildVehicle(s *Session) BundleVehicle {
	v := BundleVehicle{VIN: s.VIN}
	if s.Trans != nil {
		v.Brand = s.Trans.Brand
		v.ILevel = s.Trans.ILevel
		v.Mileage = s.Trans.Mileage
	}
	if s.Meta != nil {
		f := s.Meta.Features
		v.Series = f.Series
		v.ModelSeries = f.ModelSeries
		v.Body = f.Body
		v.Engine = f.Engine
		v.Transmission = f.Transmission
		v.ModelYear = f.ModelYear
		v.Market = f.Market
		v.Steering = f.Steering
		v.Assembly = f.AssemblyCountry
		v.CommType = s.Meta.CommType
		if v.Brand == "" {
			v.Brand = f.Brand
		}
		if v.Mileage == 0 {
			v.Mileage = s.Meta.Mileage
		}
	}
	return v
}

func buildECUs(s *Session) []BundleECU {
	if s.Trans == nil {
		return nil
	}
	ecus := make([]BundleECU, 0, len(s.Trans.ECUs))
	for _, e := range s.Trans.ECUs {
		ecus = append(ecus, BundleECU{
			Name:        e.TreeName,
			FullName:    e.FullName,
			Variant:     e.Variant,
			Bus:         e.Bus,
			Protocol:    e.Protocol,
			Supplier:    e.Supplier,
			FaultCount:  e.FaultCount,
			CommOK:      e.CommSuccess == "true",
			SoftwareIDs: e.SVK.SGBMIDs,
		})
	}
	return ecus
}

func buildFaults(s *Session) []BundleFault {
	ecuFaults := s.AllFaults()
	faults := make([]BundleFault, 0, len(ecuFaults))
	for _, ef := range ecuFaults {
		f := BundleFault{
			ECU:         ef.ECUName,
			ECUFullName: ef.ECUFullName,
			Bus:         ef.Bus,
			Code:        ef.DTC.Location,
			Description: ef.DTC.Description,
			Status:      ef.DTC.StatusText,
			Warning:     ef.DTC.WarningText,
			PCode:       ef.DTC.PCode,
		}
		if len(ef.DTC.Context) > 0 {
			f.MileageKM = ef.DTC.Context[0].Mileage
		}
		faults = append(faults, f)
	}
	return faults
}

func writeJSON(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", filepath.Base(path), err)
	}
	return os.WriteFile(path, data, 0644)
}

func writeSummary(bundleDir string, s *Session) error {
	args := []string{
		"--data-dir", bundleDir,
		"--format", "summary",
		"--output", filepath.Join(bundleDir, "summary.md"),
		"--session-date", s.Timestamp.Format("2006-01-02 15:04"),
	}

	if s.Meta != nil && s.Meta.Start != "" && s.Meta.End != "" {
		start, e1 := time.Parse(time.RFC3339Nano, s.Meta.Start)
		end, e2 := time.Parse(time.RFC3339Nano, s.Meta.End)
		if e1 == nil && e2 == nil {
			args = append(args, "--session-duration", end.Sub(start).Round(time.Minute).String())
		}
	}

	_, err := runTool("report", args...)
	return err
}
