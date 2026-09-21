package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	defaultDiagDocDBPath = `C:\EC-APPS\ISTA\SQLiteDBs\DiagDocDb.sqlite`
	defaultSQLiteDLLPath = `C:\EC-APPS\ISTA\TesterGUI\bin\Release\System.Data.SQLite.dll`
	defaultPassword      = `6505EFBDC3E5F324`
	ps32Path             = `C:\Windows\SysWOW64\WindowsPowerShell\v1.0\powershell.exe`
)

// DiagDB wraps access to the encrypted DiagDocDb.sqlite via 32-bit PowerShell.
type DiagDB struct {
	dbPath   string
	dllPath  string
	password string
}

// NewDiagDB creates a DiagDB with defaults, optionally reading the password
// from a key file on the user's desktop.
func NewDiagDB() *DiagDB {
	db := &DiagDB{
		dbPath:   defaultDiagDocDBPath,
		dllPath:  defaultSQLiteDLLPath,
		password: defaultPassword,
	}
	if pw := readKeyFile(); pw != "" {
		db.password = pw
	}
	return db
}

func NewDiagDBCustom(dbPath, dllPath, password string) *DiagDB {
	return &DiagDB{
		dbPath:   dbPath,
		dllPath:  dllPath,
		password: password,
	}
}

// readKeyFile looks for an ista_keys.txt file and extracts the DiagDocDb password.
// Checks: --keyfile flag, %USERPROFILE%\Desktop\ista_keys.txt, ./ista_keys.txt
func readKeyFile() string {
	candidates := []string{}
	for i, a := range os.Args {
		if (a == "--keyfile" || a == "-k") && i+1 < len(os.Args) {
			candidates = append([]string{os.Args[i+1]}, candidates...)
		}
	}
	home, _ := os.UserHomeDir()
	if home != "" {
		candidates = append(candidates, filepath.Join(home, "Desktop", "ista_keys.txt"))
	}
	candidates = append(candidates, "ista_keys.txt")

	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if strings.Contains(line, "DiagDocDb SQLite SEE Password:") && i+1 < len(lines) {
				pw := strings.TrimSpace(lines[i+1])
				if pw != "" {
					return pw
				}
			}
		}
	}
	return ""
}

// query executes a SQL query and returns results as a slice of maps.
// Uses 32-bit PowerShell to load System.Data.SQLite and query the encrypted DB.
func (d *DiagDB) query(sql string, args ...interface{}) ([]map[string]interface{}, error) {
	// Build PowerShell script that opens the encrypted DB and runs the query
	psScript := fmt.Sprintf(`
[void][System.Reflection.Assembly]::LoadFile('%s')
$conn = New-Object System.Data.SQLite.SQLiteConnection("Data Source=%s;Password=%s;")
$conn.Open()
$cmd = $conn.CreateCommand()
$cmd.CommandText = @'
%s
'@
$reader = $cmd.ExecuteReader()
$results = @()
while ($reader.Read()) {
    $row = @{}
    for ($i = 0; $i -lt $reader.FieldCount; $i++) {
        $name = $reader.GetName($i)
        if ($reader.IsDBNull($i)) {
            $row[$name] = $null
        } else {
            $row[$name] = $reader.GetValue($i)
        }
    }
    $results += $row
}
$reader.Close()
$conn.Close()
$results | ConvertTo-Json -Depth 3 -Compress
`, d.dllPath, d.dbPath, d.password, sql)

	cmd := exec.Command(ps32Path, "-NoProfile", "-NonInteractive", "-Command", psScript)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("ps32 error: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("ps32 exec: %w", err)
	}

	outStr := strings.TrimSpace(string(out))
	if outStr == "" || outStr == "null" {
		return nil, nil
	}

	var results []map[string]interface{}
	if err := json.Unmarshal(out, &results); err != nil {
		// Might be a single object instead of array
		var single map[string]interface{}
		if err2 := json.Unmarshal(out, &single); err2 == nil {
			return []map[string]interface{}{single}, nil
		}
		return nil, fmt.Errorf("json parse: %w (raw: %s)", err, outStr[:min(200, len(outStr))])
	}

	return results, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TableNames returns all table names in the DiagDocDb.
func (d *DiagDB) TableNames() ([]string, error) {
	rows, err := d.query("SELECT name FROM sqlite_master WHERE type='table' ORDER BY name")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, r := range rows {
		if name, ok := r["name"].(string); ok {
			names = append(names, name)
		}
	}
	return names, nil
}

// ExportTable exports a table as JSON to a file.
func (d *DiagDB) ExportTable(tableName, outputPath string, limit int) error {
	sql := fmt.Sprintf("SELECT * FROM %s", tableName)
	if limit > 0 {
		sql += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := d.query(sql)
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
		return err
	}

	return os.WriteFile(outputPath, data, 0644)
}

// ExportAllTables exports all tables as individual JSON files.
func (d *DiagDB) ExportAllTables(outputDir string) error {
	tables, err := d.TableNames()
	if err != nil {
		return err
	}

	for _, table := range tables {
		outPath := filepath.Join(outputDir, table+".json")
		fmt.Printf("Exporting %s...", table)
		if err := d.ExportTable(table, outPath, 0); err != nil {
			fmt.Printf(" ERROR: %v\n", err)
			continue
		}
		fmt.Println(" OK")
	}
	return nil
}

// LookupFaultCode searches for a fault code (P-code or BMW code) in the DiagDocDb.
func (d *DiagDB) LookupFaultCode(code string) ([]map[string]interface{}, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	sql := fmt.Sprintf(`
		SELECT f.ID, f.CODE, f.DATATYPE, f.WEIGHTING, f.RELEVANCE,
			   f.SICHERHEITSRELEVANT, f.DIAGNOSEINDEX,
			   ca.TITLE_ENGB, ca.TITLE_DEDE, ca.CONTROLDEVICE
		FROM XEP_FAULTCODES f
		LEFT JOIN CODEASSIGNMENT ca ON ca.FCODE = f.ID
		WHERE f.CODE LIKE '%%%s%%'
		LIMIT 50
	`, code)
	return d.query(sql)
}

// LookupCCMessage searches for a Check Control message.
func (d *DiagDB) LookupCCMessage(search string) ([]map[string]interface{}, error) {
	search = strings.TrimSpace(search)
	sql := fmt.Sprintf(`
		SELECT ID, CC_ID, TITLE_ENGB, TITLE_DEDE, TITLE_ENUS,
			   LONGTEXT_ENGB, CAUSE
		FROM XEP_CCMESSAGE
		WHERE TITLE_ENGB LIKE '%%%s%%' OR TITLE_DEDE LIKE '%%%s%%' OR TITLE_ENUS LIKE '%%%s%%'
		LIMIT 50
	`, search, search, search)
	return d.query(sql)
}

// LookupDiagCode searches for a diagnostic code by name.
func (d *DiagDB) LookupDiagCode(search string) ([]map[string]interface{}, error) {
	search = strings.TrimSpace(search)
	sql := fmt.Sprintf(`
		SELECT ID, NAME
		FROM XEP_DIAGCODE
		WHERE NAME LIKE '%%%s%%'
		LIMIT 50
	`, search)
	return d.query(sql)
}

// LookupPCode searches for a P-code in CODEASSIGNMENT.
func (d *DiagDB) LookupPCode(pcode string) ([]map[string]interface{}, error) {
	pcode = strings.ToUpper(strings.TrimSpace(pcode))
	sql := fmt.Sprintf(`
		SELECT ca.PCODE, ca.FCODE, ca.CONTROLDEVICE,
			   ca.TITLE_ENGB, ca.TITLE_DEDE, ca.TITLE_ZHCN,
			   f.CODE as FAULT_CODE, f.DATATYPE, f.RELEVANCE
		FROM CODEASSIGNMENT ca
		LEFT JOIN XEP_FAULTCODES f ON f.ID = ca.FCODE
		WHERE ca.PCODE LIKE '%%%s%%'
		LIMIT 50
	`, pcode)
	return d.query(sql)
}

// VINLookup performs a VIN range lookup to identify the vehicle type.
func (d *DiagDB) VINLookup(vin7 string) ([]map[string]interface{}, error) {
	vin7 = strings.ToUpper(strings.TrimSpace(vin7))
	if len(vin7) < 4 || len(vin7) > 7 {
		return nil, fmt.Errorf("VIN positions 4-7 must be 4-7 characters, got %d", len(vin7))
	}

	// VIN17_4_7 stores positions 4-7 of the VIN
	sql := fmt.Sprintf(`
		SELECT VINBANDFROM, VINBANDTO, TYPSCHLUESSEL,
			   PRODUCTIONDATEYEAR, PRODUCTIONDATEMONTH,
			   VIN17_4_7, GEARBOX_TYPE
		FROM VINRANGES
		WHERE VIN17_4_7 = '%s'
		LIMIT 100
	`, vin7)
	return d.query(sql)
}

// VINLookupByRange performs a binary search on VINRANGES for a full VIN.
// VINBANDFROM/VINBANDTO are the sequential VIN number ranges.
func (d *DiagDB) VINLookupByRange(vin string) ([]map[string]interface{}, error) {
	vin = strings.ToUpper(strings.TrimSpace(vin))
	if len(vin) != 17 {
		return nil, fmt.Errorf("VIN must be 17 characters, got %d", len(vin))
	}

	// Extract VIN positions 4-7 (model/type code)
	vin47 := vin[3:7]
	// Extract the sequential part (last6 digits)
	seqPart := vin[11:17]

	sql := fmt.Sprintf(`
		SELECT VINBANDFROM, VINBANDTO, TYPSCHLUESSEL,
			   PRODUCTIONDATEYEAR, PRODUCTIONDATEMONTH,
			   VIN17_4_7, GEARBOX_TYPE
		FROM VINRANGES
		WHERE VIN17_4_7 = '%s'
		  AND VINBANDFROM <= '%s'
		  AND VINBANDTO >= '%s'
		LIMIT 10
	`, vin47, seqPart, seqPart)
	return d.query(sql)
}
