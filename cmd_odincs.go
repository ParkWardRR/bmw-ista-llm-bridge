package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runOdincs(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: ista-bridge odincs <dll-or-directory>")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Extracts .NET assembly public key tokens from PE/CLI DLLs.")
		fmt.Fprintln(os.Stderr, "Uses the Odin-based ista-keyextract tool for native PE parsing.")
		os.Exit(1)
	}

	target := args[0]
	info, err := os.Stat(target)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var dlls []string
	if info.IsDir() {
		entries, err := os.ReadDir(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading dir: %v\n", err)
			os.Exit(1)
		}
		for _, e := range entries {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if !e.IsDir() && (ext == ".dll" || ext == ".exe") {
				dlls = append(dlls, filepath.Join(target, e.Name()))
			}
		}
	} else {
		dlls = append(dlls, target)
	}

	for _, dll := range dlls {
		token, err := extractKeyWithOdin(dll)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%-55s ERROR: %v\n", filepath.Base(dll), err)
			continue
		}
		if token == "" {
			fmt.Printf("%-55s (no strong name)\n", filepath.Base(dll))
		} else {
			fmt.Printf("%-55s %s\n", filepath.Base(dll), token)
		}
	}
}

type keyExtractResult struct {
	Source        string `json:"source"`
	Path          string `json:"path"`
	PublicKeyToken string `json:"public_key_token"`
	DBPassword    string `json:"db_password"`
}

func extractKeyWithOdin(dllPath string) (string, error) {
	out, err := runTool("keyextract", "--dll", dllPath)
	if err != nil {
		return "", err
	}

	var result keyExtractResult
	if err := json.Unmarshal(out, &result); err != nil {
		return "", fmt.Errorf("parse keyextract output: %w", err)
	}

	return result.PublicKeyToken, nil
}
