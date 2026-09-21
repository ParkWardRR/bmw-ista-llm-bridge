package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func runVINLookup(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: istahelp vin <vin>")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Lookup vehicle information from VIN using DiagDocDb VINRANGES.")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Examples:")
		fmt.Fprintln(os.Stderr, "  istahelp vin WBAJB1C52JB123456    # Full 17-digit VIN")
		fmt.Fprintln(os.Stderr, "  istahelp vin JB1C                  # VIN positions 4-7 only")
		os.Exit(1)
	}

	db := NewDiagDB()
	vin := strings.ToUpper(strings.TrimSpace(args[0]))

	var results []map[string]interface{}
	var err error

	if len(vin) == 17 {
		// Full VIN - lookup by positions 4-7 and sequential range
		fmt.Printf("Looking up VIN: %s\n", vin)
		fmt.Printf("  Model code (pos 4-7): %s\n", vin[3:7])
		fmt.Printf("  Sequential (pos 12-17): %s\n\n", vin[11:17])

		results, err = db.VINLookupByRange(vin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if len(results) == 0 {
			// Fall back to just positions 4-7
			fmt.Println("No exact range match. Trying positions 4-7 only...")
			results, err = db.VINLookup(vin[3:7])
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		}
	} else if len(vin) >= 4 && len(vin) <= 7 {
		// Just positions 4-7
		fmt.Printf("Looking up model code: %s\n\n", vin)
		results, err = db.VINLookup(vin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		fmt.Fprintln(os.Stderr, "VIN must be 17 characters or 4-7 characters (positions 4-7)")
		os.Exit(1)
	}

	if len(results) == 0 {
		fmt.Println("No matching VIN ranges found.")
		return
	}

	// Display results
	fmt.Printf("Found %d matching VIN range(s):\n\n", len(results))
	for i, r := range results {
		fmt.Printf("--- Match %d ---\n", i+1)
		if v, ok := r["TYPSCHLUESSEL"]; ok && v != nil {
			fmt.Printf("  Type Key:      %v\n", v)
		}
		if v, ok := r["VIN17_4_7"]; ok && v != nil {
			fmt.Printf("  VIN 4-7:       %v\n", v)
		}
		if v, ok := r["VINBANDFROM"]; ok && v != nil {
			fmt.Printf("  Range From:    %v\n", v)
		}
		if v, ok := r["VINBANDTO"]; ok && v != nil {
			fmt.Printf("  Range To:      %v\n", v)
		}
		if v, ok := r["PRODUCTIONDATEYEAR"]; ok && v != nil {
			fmt.Printf("  Prod. Year:    %v\n", v)
		}
		if v, ok := r["PRODUCTIONDATEMONTH"]; ok && v != nil {
			fmt.Printf("  Prod. Month:   %v\n", v)
		}
		if v, ok := r["GEARBOX_TYPE"]; ok && v != nil {
			fmt.Printf("  Gearbox:       %v\n", v)
		}
		fmt.Println()
	}

	// Also output JSON if --json flag
	for _, a := range args {
		if a == "--json" || a == "-j" {
			data, _ := json.MarshalIndent(results, "", "  ")
			fmt.Println(string(data))
			return
		}
	}
}
