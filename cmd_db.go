package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func runDB(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: ista-bridge db <subcommand>")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Subcommands:")
		fmt.Fprintln(os.Stderr, "  tables              List all tables in DiagDocDb")
		fmt.Fprintln(os.Stderr, "  export <table>      Export a table as JSON")
		fmt.Fprintln(os.Stderr, "  export-all <dir>    Export all tables to directory")
		fmt.Fprintln(os.Stderr, "  export-lookup       Export lookup data for satellite tools")
		fmt.Fprintln(os.Stderr, "  query <sql>         Run raw SQL query")
		fmt.Fprintln(os.Stderr, "  schema <table>      Show table schema")
		fmt.Fprintln(os.Stderr, "  count <table>       Count rows in a table")
		os.Exit(1)
	}

	switch args[0] {
	case "export-lookup":
		cfg := loadConfig()
		if err := exportLookupData(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("\nLookup data exported. VIN and fault lookups will now use")
		fmt.Println("the Zig and Gleam satellite tools for faster offline search.")
		return
	}

	db := NewDiagDB()

	switch args[0] {
	case "tables":
		names, err := db.TableNames()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		for _, n := range names {
			fmt.Println(n)
		}

	case "export":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: istahelp db export <table> [output.json]")
			os.Exit(1)
		}
		table := args[1]
		outPath := table + ".json"
		if len(args) >= 3 {
			outPath = args[2]
		}
		if err := db.ExportTable(table, outPath, 0); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Exported %s to %s\n", table, outPath)

	case "export-all":
		outDir := "db-export"
		if len(args) >= 2 {
			outDir = args[1]
		}
		if err := db.ExportAllTables(outDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

	case "query":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: istahelp db query <sql>")
			os.Exit(1)
		}
		sql := strings.Join(args[1:], " ")
		rows, err := db.query(sql)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		data, _ := json.MarshalIndent(rows, "", "  ")
		fmt.Println(string(data))

	case "schema":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: istahelp db schema <table>")
			os.Exit(1)
		}
		rows, err := db.query(fmt.Sprintf("PRAGMA table_info(%s)", args[1]))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		for _, r := range rows {
			fmt.Printf("  %-30s %s\n", r["name"], r["type"])
		}

	case "count":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: istahelp db count <table>")
			os.Exit(1)
		}
		rows, err := db.query(fmt.Sprintf("SELECT count(*) as cnt FROM %s", args[1]))
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(rows) > 0 {
			fmt.Println(rows[0]["cnt"])
		}

	default:
		fmt.Fprintf(os.Stderr, "Unknown subcommand: %s\n", args[0])
		os.Exit(1)
	}
}
