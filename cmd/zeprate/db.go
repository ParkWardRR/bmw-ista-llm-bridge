package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// DBConfig holds the connection details for the encrypted DiagDocDb.
type DBConfig struct {
	DBPath   string
	DLLPath  string
	Password string
	PS32     string
}

// DiagDB wraps access to the encrypted DiagDocDb.sqlite via 32-bit PowerShell.
type DiagDB struct {
	cfg DBConfig
}

// TableInfo holds name and row count for a table.
type TableInfo struct {
	Name string
	Rows int
}

func defaultDBConfig() DBConfig {
	istaDir := `C:\EC-APPS\ISTA`
	return DBConfig{
		DBPath:  filepath.Join(istaDir, "SQLiteDBs", "DiagDocDb.sqlite"),
		DLLPath: filepath.Join(istaDir, "TesterGUI", "bin", "Release", "System.Data.SQLite.dll"),
		PS32:    `C:\Windows\SysWOW64\WindowsPowerShell\v1.0\powershell.exe`,
	}
}

// DiscoverPassword extracts the public key token from the Rheingold assembly.
func DiscoverPassword(cfg DBConfig) (string, error) {
	dllPath := filepath.Join(filepath.Dir(cfg.DLLPath), "RheingoldCoreFramework.dll")
	if _, err := os.Stat(dllPath); err != nil {
		dllPath = filepath.Join(filepath.Dir(cfg.DLLPath), "RheingoldDatabaseSQLiteConnector.dll")
	}

	psScript := fmt.Sprintf(`try {
  $dll = [System.Reflection.Assembly]::LoadFile('%s')
  $token = $dll.GetName().GetPublicKeyToken()
  if ($token) { [BitConverter]::ToString($token).Replace('-','').ToUpper() }
} catch { "" }`, dllPath)

	cmd := exec.Command(cfg.PS32, "-NoProfile", "-NonInteractive", "-Command", psScript)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("discover password: %w", err)
	}

	result := strings.TrimSpace(string(out))
	if result == "" {
		return "", fmt.Errorf("no public key token found")
	}
	if _, err := hex.DecodeString(result); err != nil {
		return "", fmt.Errorf("invalid hex token: %s", result)
	}
	return result, nil
}

func NewDiagDB(cfg DBConfig) *DiagDB {
	return &DiagDB{cfg: cfg}
}

// query executes SQL and returns results as a slice of maps.
func (d *DiagDB) query(sql string) ([]map[string]interface{}, error) {
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
        if ($reader.IsDBNull($i)) { $row[$name] = $null }
        else { $row[$name] = $reader.GetValue($i) }
    }
    $results += $row
}
$reader.Close()
$conn.Close()
$results | ConvertTo-Json -Depth 3 -Compress
`, d.cfg.DLLPath, d.cfg.DBPath, d.cfg.Password, sql)

	cmd := exec.Command(d.cfg.PS32, "-NoProfile", "-NonInteractive", "-Command", psScript)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("%s", string(exitErr.Stderr))
		}
		return nil, err
	}

	outStr := strings.TrimSpace(string(out))
	if outStr == "" || outStr == "null" {
		return nil, nil
	}

	var results []map[string]interface{}
	if err := json.Unmarshal(out, &results); err != nil {
		var single map[string]interface{}
		if err2 := json.Unmarshal(out, &single); err2 == nil {
			return []map[string]interface{}{single}, nil
		}
		return nil, fmt.Errorf("json: %w", err)
	}
	return results, nil
}

// TableNames returns all table names.
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

// TableInfos returns tables with row counts.
func (d *DiagDB) TableInfos(progress func(string, int, int)) ([]TableInfo, error) {
	names, err := d.TableNames()
	if err != nil {
		return nil, err
	}

	var infos []TableInfo
	for i, name := range names {
		if progress != nil {
			progress(name, i+1, len(names))
		}
		rows, err := d.query(fmt.Sprintf("SELECT count(*) as c FROM \"%s\"", name))
		cnt := -1
		if err == nil && len(rows) > 0 {
			if c, ok := rows[0]["c"].(float64); ok {
				cnt = int(c)
			}
		}
		infos = append(infos, TableInfo{Name: name, Rows: cnt})
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Rows > infos[j].Rows
	})

	return infos, nil
}

// ExportTable exports a table as JSON.
func (d *DiagDB) ExportTable(name, path string) (int, error) {
	rows, err := d.query(fmt.Sprintf("SELECT * FROM \"%s\"", name))
	if err != nil {
		return 0, err
	}
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return 0, err
	}
	os.MkdirAll(filepath.Dir(path), 0755)
	return len(rows), os.WriteFile(path, data, 0644)
}

// LookupPCode searches for a P-code.
func (d *DiagDB) LookupPCode(code string) ([]map[string]interface{}, error) {
	return d.query(fmt.Sprintf(`
		SELECT ca.PCODE, ca.FCODE, ca.CONTROLDEVICE,
			   ca.TITLE_ENGB, ca.TITLE_DEDE, ca.TITLE_ZHCN,
			   f.CODE as FAULT_CODE, f.DATATYPE, f.RELEVANCE
		FROM CODEASSIGNMENT ca
		LEFT JOIN XEP_FAULTCODES f ON f.ID = ca.FCODE
		WHERE ca.PCODE LIKE '%%%s%%' LIMIT 50`, strings.ToUpper(code)))
}

// LookupFaultCode searches for a BMW fault code.
func (d *DiagDB) LookupFaultCode(code string) ([]map[string]interface{}, error) {
	return d.query(fmt.Sprintf(`
		SELECT f.ID, f.CODE, f.DATATYPE, f.WEIGHTING, f.RELEVANCE,
			   f.SICHERHEITSRELEVANT, f.DIAGNOSEINDEX,
			   ca.TITLE_ENGB, ca.TITLE_DEDE, ca.CONTROLDEVICE
		FROM XEP_FAULTCODES f
		LEFT JOIN CODEASSIGNMENT ca ON ca.FCODE = f.ID
		WHERE f.CODE LIKE '%%%s%%' LIMIT 50`, strings.ToUpper(code)))
}

// LookupCCMessage searches for a Check Control message.
func (d *DiagDB) LookupCCMessage(search string) ([]map[string]interface{}, error) {
	return d.query(fmt.Sprintf(`
		SELECT ID, CC_ID, TITLE_ENGB, TITLE_DEDE, TITLE_ENUS, CAUSE
		FROM XEP_CCMESSAGE
		WHERE TITLE_ENGB LIKE '%%%s%%' OR TITLE_DEDE LIKE '%%%s%%' OR TITLE_ENUS LIKE '%%%s%%'
		LIMIT 50`, search, search, search))
}

// LookupDiagCode searches for a diagnostic code.
func (d *DiagDB) LookupDiagCode(search string) ([]map[string]interface{}, error) {
	return d.query(fmt.Sprintf(`
		SELECT ID, NAME FROM XEP_DIAGCODE
		WHERE NAME LIKE '%%%s%%' LIMIT 50`, search))
}

// VINLookup searches VINRANGES by positions 4-7.
func (d *DiagDB) VINLookup(vin47 string) ([]map[string]interface{}, error) {
	return d.query(fmt.Sprintf(`
		SELECT VINBANDFROM, VINBANDTO, TYPSCHLUESSEL,
			   PRODUCTIONDATEYEAR, PRODUCTIONDATEMONTH, VIN17_4_7, GEARBOX_TYPE
		FROM VINRANGES WHERE VIN17_4_7 = '%s' LIMIT 100`, strings.ToUpper(vin47)))
}

// VINLookupByRange searches VINRANGES by full VIN.
func (d *DiagDB) VINLookupByRange(vin string) ([]map[string]interface{}, error) {
	vin = strings.ToUpper(vin)
	if len(vin) != 17 {
		return nil, fmt.Errorf("VIN must be 17 characters")
	}
	return d.query(fmt.Sprintf(`
		SELECT VINBANDFROM, VINBANDTO, TYPSCHLUESSEL,
			   PRODUCTIONDATEYEAR, PRODUCTIONDATEMONTH, VIN17_4_7, GEARBOX_TYPE
		FROM VINRANGES WHERE VIN17_4_7 = '%s'
		  AND VINBANDFROM <= '%s' AND VINBANDTO >= '%s'
		LIMIT 10`, vin[3:7], vin[11:17], vin[11:17]))
}
