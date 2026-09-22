package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func runLookup(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: ista-bridge lookup <type> <search>")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Types:")
		fmt.Fprintln(os.Stderr, "  pcode <code>     Lookup P-code (e.g., P0300)")
		fmt.Fprintln(os.Stderr, "  fault <code>     Lookup BMW fault code")
		fmt.Fprintln(os.Stderr, "  cc <text>        Search Check Control messages")
		fmt.Fprintln(os.Stderr, "  diag <text>      Search diagnostic codes")
		fmt.Fprintln(os.Stderr, "  fault-id <id>    Lookup fault by database ID")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Uses the Gleam-based ista-faultlookup for pcode/fault searches,")
		fmt.Fprintln(os.Stderr, "falling back to DiagDocDb for cc/diag/fault-id or when lookup data")
		fmt.Fprintln(os.Stderr, "is not exported.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Options:")
		fmt.Fprintln(os.Stderr, "  --json           Output raw JSON")
		os.Exit(1)
	}

	searchType := args[0]
	searchTerm := args[1]
	jsonOutput := false

	for _, a := range args {
		if a == "--json" || a == "-j" {
			jsonOutput = true
		}
	}

	switch searchType {
	case "pcode", "p-code", "p", "fault", "f", "dtc":
		cfg := loadConfig()
		dataDir := lookupDataDir(cfg)
		if _, err := os.Stat(filepath.Join(dataDir, "faultcodes.json")); err == nil {
			lookupGleam(searchTerm, dataDir, jsonOutput)
		} else {
			lookupDB(searchType, searchTerm, jsonOutput)
		}

	case "cc", "check-control", "checkcontrol", "diag", "diagnostic", "d", "fault-id", "fid":
		lookupDB(searchType, searchTerm, jsonOutput)

	default:
		fmt.Fprintf(os.Stderr, "Unknown lookup type: %s\n", searchType)
		os.Exit(1)
	}
}

type gleamLookupResult struct {
	FaultCodes []gleamFaultCode `json:"fault_codes"`
	PCodes     []gleamPCode     `json:"pcodes"`
}

type gleamFaultCode struct {
	Code           string `json:"code"`
	SAECode        string `json:"sae_code"`
	Title          string `json:"title"`
	ECUVariant     string `json:"ecu_variant"`
	ECUGroup       string `json:"ecu_group"`
	Weighting      int    `json:"weighting"`
	SafetyRelevant bool   `json:"safety_relevant"`
}

type gleamPCode struct {
	PCode  string `json:"pcode"`
	FCode  int    `json:"fcode"`
	Device string `json:"device"`
	Title  string `json:"title"`
}

func lookupGleam(search, dataDir string, jsonOutput bool) {
	var result gleamLookupResult
	err := runToolJSON("faultlookup", &result, "--json", search)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ista-faultlookup: %v\nFalling back to database...\n\n", err)
		lookupDB("fault", search, jsonOutput)
		return
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		return
	}

	totalFaults := len(result.FaultCodes)
	totalPCodes := len(result.PCodes)

	if totalFaults == 0 && totalPCodes == 0 {
		fmt.Printf("No results found for: %s\n", search)
		return
	}

	if totalFaults > 0 {
		fmt.Printf("=== Fault Codes (%d) ===\n\n", totalFaults)
		for i, f := range result.FaultCodes {
			safety := "no"
			if f.SafetyRelevant {
				safety = "YES"
			}
			fmt.Printf("[%d] Code: %s | SAE: %s | ECU: %s (%s)\n", i+1, f.Code, f.SAECode, f.ECUVariant, f.ECUGroup)
			fmt.Printf("    %s\n", f.Title)
			fmt.Printf("    Weighting: %d | Safety: %s\n\n", f.Weighting, safety)
		}
	}

	if totalPCodes > 0 {
		fmt.Printf("=== P-Codes (%d) ===\n\n", totalPCodes)
		for i, p := range result.PCodes {
			fmt.Printf("[%d] %s -> F%d | Device: %s\n", i+1, p.PCode, p.FCode, p.Device)
			fmt.Printf("    %s\n\n", p.Title)
		}
	}
}

func lookupDB(searchType, searchTerm string, jsonOutput bool) {
	db := NewDiagDB()

	var results []map[string]interface{}
	var err error
	var label string

	switch searchType {
	case "pcode", "p-code", "p":
		label = "P-code"
		results, err = db.LookupPCode(searchTerm)
	case "fault", "f", "dtc":
		label = "Fault code"
		results, err = db.LookupFaultCode(searchTerm)
	case "cc", "check-control", "checkcontrol":
		label = "Check Control message"
		results, err = db.LookupCCMessage(searchTerm)
	case "diag", "diagnostic", "d":
		label = "Diagnostic code"
		results, err = db.LookupDiagCode(searchTerm)
	case "fault-id", "fid":
		label = "Fault by ID"
		sql := fmt.Sprintf(`
			SELECT f.ID, f.CODE, f.DATATYPE, f.WEIGHTING, f.RELEVANCE,
				   f.SICHERHEITSRELEVANT, f.DIAGNOSEINDEX,
				   ca.TITLE_ENGB, ca.TITLE_DEDE, ca.CONTROLDEVICE
			FROM XEP_FAULTCODES f
			LEFT JOIN CODEASSIGNMENT ca ON ca.FCODE = f.ID
			WHERE f.ID = %s
		`, searchTerm)
		results, err = db.query(sql)
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
		fmt.Printf("No %s results found for: %s\n", label, searchTerm)
		return
	}

	fmt.Printf("Found %d %s result(s) for \"%s\":\n\n", len(results), label, searchTerm)
	for i, r := range results {
		fmt.Printf("--- Result %d ---\n", i+1)
		for k, v := range r {
			if v != nil && v != "" {
				s := fmt.Sprintf("%v", v)
				if len(s) > 200 {
					s = s[:200] + "..."
				}
				fmt.Printf("  %-20s %s\n", k+":", s)
			}
		}
		fmt.Println()
	}
}
