package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func runLookup(args []string) {
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: istahelp lookup <type> <search>")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Types:")
		fmt.Fprintln(os.Stderr, "  pcode <code>     Lookup P-code (e.g., P0300)")
		fmt.Fprintln(os.Stderr, "  fault <code>     Lookup BMW fault code")
		fmt.Fprintln(os.Stderr, "  cc <text>        Search Check Control messages")
		fmt.Fprintln(os.Stderr, "  diag <text>      Search diagnostic codes")
		fmt.Fprintln(os.Stderr, "  fault-id <id>    Lookup fault by database ID")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Options:")
		fmt.Fprintln(os.Stderr, "  --json           Output raw JSON")
		os.Exit(1)
	}

	db := NewDiagDB()
	searchType := args[0]
	searchTerm := args[1]
	jsonOutput := false

	for _, a := range args {
		if a == "--json" || a == "-j" {
			jsonOutput = true
		}
	}

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

	default:
		fmt.Fprintf(os.Stderr, "Unknown lookup type: %s\n", searchType)
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
