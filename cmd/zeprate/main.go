package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--help", "-h":
			fmt.Println("zeprate — BMW ISTA DiagDocDb Explorer")
			fmt.Println()
			fmt.Println("Unlocks and explores the encrypted DiagDocDb.sqlite (7.6 GB)")
			fmt.Println("from an existing ISTA installation via a terminal UI.")
			fmt.Println()
			fmt.Println("Usage:")
			fmt.Println("  zeprate              Launch interactive TUI")
			fmt.Println("  zeprate --discover   Show discovered DB password")
			fmt.Println("  zeprate --export DIR Export all tables to JSON")
			fmt.Println()
			fmt.Println("The tool auto-detects the ISTA install at C:\\EC-APPS\\ISTA")
			fmt.Println("and extracts the encryption key from the Rheingold DLLs.")
			return
		case "--discover":
			cfg := defaultDBConfig()
			pw, err := DiscoverPassword(cfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Password: %s\n", pw)
			fmt.Printf("DB:       %s\n", cfg.DBPath)
			return
		case "--export":
			dir := "db-export"
			if len(os.Args) > 2 {
				dir = os.Args[2]
			}
			runExport(dir)
			return
		}
	}

	runTUI()
}

func runExport(dir string) {
	cfg := defaultDBConfig()
	pw, err := DiscoverPassword(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Password discovery failed: %v\n", err)
		os.Exit(1)
	}
	cfg.Password = pw
	db := NewDiagDB(cfg)

	fmt.Printf("Exporting to %s...\n", dir)
	tables, err := db.TableNames()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	for _, t := range tables {
		path := dir + "/" + t + ".json"
		n, err := db.ExportTable(t, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", t, err)
		} else {
			fmt.Printf("  %s: %d rows\n", t, n)
		}
	}
	fmt.Println("Done.")
}

func runTUI() {
	p := tea.NewProgram(newModel(), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
