package main

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func runOdincs(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "Usage: istahelp odincs <dll-or-directory>")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Extracts .NET assembly public key tokens from PE/CLI DLLs.")
		fmt.Fprintln(os.Stderr, "Uses 32-bit PowerShell to load the assembly and read the token.")
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
			if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".dll") {
				dlls = append(dlls, filepath.Join(target, e.Name()))
			}
		}
	} else {
		dlls = append(dlls, target)
	}

	ps32 := `C:\Windows\SysWOW64\WindowsPowerShell\v1.0\powershell.exe`

	for _, dll := range dlls {
		token, err := extractTokenPS32(ps32, dll)
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

func extractTokenPS32(ps32, dllPath string) (string, error) {
	psScript := fmt.Sprintf(`try {
  $dll = [System.Reflection.Assembly]::LoadFile('%s')
  $token = $dll.GetName().GetPublicKeyToken()
  if ($token) { [BitConverter]::ToString($token).Replace('-','').ToLower() }
} catch { "" }`, dllPath)

	cmd := exec.Command(ps32, "-NoProfile", "-NonInteractive", "-Command", psScript)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ps32: %w", err)
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return "", nil
	}

	// Validate it looks like a hex token
	if _, err := hex.DecodeString(result); err != nil {
		return "", fmt.Errorf("invalid hex token: %s", result)
	}

	return result, nil
}
