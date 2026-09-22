package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var satelliteTools = map[string]struct {
	subdir string
	binary string
}{
	"keyextract":  {subdir: "tools/odin/db_bridge", binary: "ista-keyextract"},
	"vinlookup":   {subdir: "tools/zig/data_processor/zig-out/bin", binary: "ista-vinlookup"},
	"faultlookup": {subdir: "tools/gleam/fault_lookup/build/erlang-shipment", binary: "entrypoint"},
	"report":      {subdir: "tools/nim/report_gen", binary: "ista-report"},
	"enet":        {subdir: "tools/nim/enet_client", binary: "ista-enet"},
	"import":      {subdir: "tools/nim/data_import", binary: "ista-import"},
	"context":     {subdir: "tools/nim/context_builder", binary: "ista-context"},
}

func findTool(name string) (string, bool) {
	info, ok := satelliteTools[name]
	if !ok {
		return "", false
	}

	selfPath, _ := os.Executable()
	selfDir := filepath.Dir(selfPath)

	candidates := []string{
		filepath.Join(selfDir, info.binary+".exe"),
		filepath.Join(selfDir, info.binary),
		filepath.Join(selfDir, info.subdir, info.binary+".exe"),
		filepath.Join(selfDir, info.subdir, info.binary),
	}

	cwd, _ := os.Getwd()
	if cwd != "" {
		candidates = append(candidates,
			filepath.Join(cwd, info.subdir, info.binary+".exe"),
			filepath.Join(cwd, info.subdir, info.binary),
			filepath.Join(cwd, info.binary+".exe"),
			filepath.Join(cwd, info.binary),
		)
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, true
		}
	}

	if p, err := exec.LookPath(info.binary); err == nil {
		return p, true
	}

	return "", false
}

func runTool(name string, args ...string) ([]byte, error) {
	path, found := findTool(name)
	if !found {
		return nil, fmt.Errorf("%s: tool not found (build it or add to PATH)", name)
	}
	cmd := exec.Command(path, args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return out, nil
}

func runToolJSON(name string, result any, args ...string) error {
	out, err := runTool(name, args...)
	if err != nil {
		return err
	}
	return json.Unmarshal(out, result)
}

func lookupDataDir(cfg Config) string {
	dir, err := configDir()
	if err != nil {
		return "data"
	}
	return filepath.Join(dir, "data")
}

func exportLookupData(cfg Config) error {
	db := NewDiagDB()
	dir := lookupDataDir(cfg)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	exports := []struct {
		table string
		file  string
		sql   string
	}{
		{
			table: "XEP_FAULTCODES + CODEASSIGNMENT",
			file:  "faultcodes.json",
			sql: `SELECT f.CODE as code, COALESCE(ca.SAE_CODE,'') as sae_code,
				COALESCE(ca.TITLE_ENGB, ca.TITLE_DEDE, '') as title,
				COALESCE(ca.CONTROLDEVICE,'') as ecu_variant,
				COALESCE(f.ECU_GRUPPE,'') as ecu_group,
				COALESCE(f.WEIGHTING,0) as weighting,
				CASE WHEN f.SICHERHEITSRELEVANT = 1 THEN 'true' ELSE 'false' END as safety_relevant
				FROM XEP_FAULTCODES f
				LEFT JOIN CODEASSIGNMENT ca ON ca.FCODE = f.ID
				WHERE ca.TITLE_ENGB IS NOT NULL
				LIMIT 50000`,
		},
		{
			table: "CODEASSIGNMENT (pcodes)",
			file:  "pcodes.json",
			sql: `SELECT DISTINCT ca.SAE_CODE as pcode, ca.FCODE as fcode,
				COALESCE(ca.CONTROLDEVICE,'') as device,
				COALESCE(ca.TITLE_ENGB, ca.TITLE_DEDE, '') as title
				FROM CODEASSIGNMENT ca
				WHERE ca.SAE_CODE IS NOT NULL AND ca.SAE_CODE != ''
				LIMIT 50000`,
		},
		{
			table: "VINRANGES",
			file:  "vinranges.json",
			sql: `SELECT VIN17_4_7 as vin_4_7, VINBANDFROM as "from", VINBANDTO as "to",
				COALESCE(TYPSCHLUESSEL,'') as type_key,
				COALESCE(PRODUCTIONDATEYEAR,'') as year,
				COALESCE(PRODUCTIONDATEMONTH,'') as month,
				COALESCE(GEARBOX_TYPE,'') as gearbox
				FROM VINRANGES
				ORDER BY VIN17_4_7, VINBANDFROM`,
		},
	}

	for _, e := range exports {
		fmt.Printf("Exporting %s...\n", e.table)
		results, err := db.query(e.sql)
		if err != nil {
			return fmt.Errorf("export %s: %w", e.table, err)
		}

		// Convert safety_relevant string to bool for faultcodes
		if e.file == "faultcodes.json" {
			for i := range results {
				if v, ok := results[i]["safety_relevant"]; ok {
					results[i]["safety_relevant"] = strings.EqualFold(fmt.Sprint(v), "true")
				}
				if v, ok := results[i]["weighting"]; ok {
					switch w := v.(type) {
					case float64:
						results[i]["weighting"] = int(w)
					case string:
						results[i]["weighting"] = 0
					}
				}
			}
		}
		if e.file == "pcodes.json" {
			for i := range results {
				if v, ok := results[i]["fcode"]; ok {
					switch w := v.(type) {
					case float64:
						results[i]["fcode"] = int(w)
					}
				}
			}
		}

		data, err := json.MarshalIndent(results, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal %s: %w", e.file, err)
		}

		outPath := filepath.Join(dir, e.file)
		if err := os.WriteFile(outPath, data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", e.file, err)
		}
		fmt.Printf("  → %s (%d records)\n", outPath, len(results))
	}

	return nil
}
