package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runVINLookup(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: ista-bridge vin <vin>")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Lookup vehicle information from VIN.")
		fmt.Fprintln(os.Stderr, "Uses the Zig-based ista-vinlookup for fast binary search,")
		fmt.Fprintln(os.Stderr, "falling back to DiagDocDb if lookup data is not exported.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  ista-bridge vin WBAJB1C52JB123456    # Full 17-digit VIN")
		fmt.Fprintln(os.Stderr, "  ista-bridge vin JB1C                  # VIN positions 4-7 only")
		os.Exit(1)
	}

	vin := strings.ToUpper(strings.TrimSpace(args[0]))
	jsonOutput := false
	for _, a := range args[1:] {
		if a == "--json" || a == "-j" {
			jsonOutput = true
		}
	}

	cfg := loadConfig()
	dataDir := lookupDataDir(cfg)
	dataFile := filepath.Join(dataDir, "vinranges.json")

	if _, err := os.Stat(dataFile); err == nil {
		vinLookupZig(vin, dataFile, jsonOutput)
	} else {
		vinLookupDB(vin, jsonOutput)
	}
}

type zigVINResult struct {
	VIN     string     `json:"vin"`
	Last7   string     `json:"last7"`
	Matches []zigMatch `json:"matches"`
}

type zigMatch struct {
	TypeKey         string `json:"type_key"`
	VIN47           string `json:"vin_4_7"`
	ProductionYear  string `json:"production_year"`
	ProductionMonth string `json:"production_month"`
	GearboxType     string `json:"gearbox_type"`
}

func vinLookupZig(vin, dataFile string, jsonOutput bool) {
	var result zigVINResult
	err := runToolJSON("vinlookup", &result, "--data", dataFile, vin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ista-vinlookup failed: %v\nFalling back to database...\n\n", err)
		vinLookupDB(vin, jsonOutput)
		return
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(result.Matches) == 0 {
		fmt.Println("No matching VIN ranges found.")
		return
	}

	fmt.Printf("VIN: %s (last 7: %s)\n", result.VIN, result.Last7)
	fmt.Printf("Found %d match(es):\n\n", len(result.Matches))
	for i, m := range result.Matches {
		fmt.Printf("--- Match %d ---\n", i+1)
		fmt.Printf("  Type Key:      %s\n", m.TypeKey)
		fmt.Printf("  VIN 4-7:       %s\n", m.VIN47)
		fmt.Printf("  Prod. Year:    %s\n", m.ProductionYear)
		fmt.Printf("  Prod. Month:   %s\n", m.ProductionMonth)
		fmt.Printf("  Gearbox:       %s\n", m.GearboxType)
		fmt.Println()
	}
}

func vinLookupDB(vin string, jsonOutput bool) {
	db := NewDiagDB()

	var results []map[string]interface{}
	var err error

	if len(vin) == 17 {
		fmt.Printf("Looking up VIN: %s (via DiagDocDb)\n", vin)
		results, err = db.VINLookupByRange(vin)
		if err == nil && len(results) == 0 {
			results, err = db.VINLookup(vin[3:7])
		}
	} else if len(vin) >= 4 && len(vin) <= 7 {
		fmt.Printf("Looking up model code: %s (via DiagDocDb)\n\n", vin)
		results, err = db.VINLookup(vin)
	} else {
		fmt.Fprintln(os.Stderr, "VIN must be 17 characters or 4-7 characters (positions 4-7)")
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(results, "", "  ")
		fmt.Println(string(data))
		return
	}

	if len(results) == 0 {
		fmt.Println("No matching VIN ranges found.")
		return
	}

	fmt.Printf("Found %d matching VIN range(s):\n\n", len(results))
	for i, r := range results {
		fmt.Printf("--- Match %d ---\n", i+1)
		for _, key := range []string{"TYPSCHLUESSEL", "VIN17_4_7", "VINBANDFROM", "VINBANDTO",
			"PRODUCTIONDATEYEAR", "PRODUCTIONDATEMONTH", "GEARBOX_TYPE"} {
			if v, ok := r[key]; ok && v != nil {
				fmt.Printf("  %-14s %v\n", key+":", v)
			}
		}
		fmt.Println()
	}
}
