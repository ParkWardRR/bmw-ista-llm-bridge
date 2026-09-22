package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"
)

type ZipLogContents struct {
	Files    []ZipLogFile    `json:"files"`
	ECUKom   *ECUKomData     `json:"ecukom,omitempty"`
	Timeline []TimelineEvent `json:"timeline,omitempty"`
}

type ZipLogFile struct {
	Name string `json:"name"`
	Size int64  `json:"size_bytes"`
}

type ECUKomData struct {
	Jobs []ECUKomJob `json:"jobs"`
}

type ECUKomJob struct {
	ECU     string         `json:"ecu"`
	SGBD    string         `json:"sgbd,omitempty"`
	JobName string         `json:"job_name"`
	Results []ECUKomResult `json:"results,omitempty"`
}

type ECUKomResult struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Format string `json:"format,omitempty"`
}

type TimelineEvent struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level,omitempty"`
	Source    string `json:"source,omitempty"`
	Message   string `json:"message"`
}

func parseZipLog(zipPath string) (*ZipLogContents, error) {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, fmt.Errorf("open zip.log: %w", err)
	}
	defer r.Close()

	result := &ZipLogContents{}

	for _, f := range r.File {
		result.Files = append(result.Files, ZipLogFile{
			Name: f.Name,
			Size: int64(f.UncompressedSize64),
		})

		data, err := readZipEntry(f)
		if err != nil {
			continue
		}

		switch {
		case f.Name == "IstaOperation.log":
			result.Timeline = parseIstaOperationLog(data)
		case isECUKomFile(f.Name):
			if ek, err := parseECUKomXML(data); err == nil {
				result.ECUKom = ek
			}
		}
	}

	return result, nil
}

func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func isECUKomFile(name string) bool {
	if !strings.HasSuffix(strings.ToLower(name), ".xml") {
		return false
	}
	upper := strings.ToUpper(name)
	return !strings.HasPrefix(upper, "RG_") &&
		!strings.HasPrefix(name, "[") &&
		!strings.Contains(strings.ToLower(name), "behdat")
}

// parseECUKomXML uses generic XML tree walking to extract EDIABAS job results.
// The format varies across ISTA versions, so we look for common patterns:
// elements containing "job" in their name, with child result elements.
func parseECUKomXML(data []byte) (*ECUKomData, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	result := &ECUKomData{}
	var currentECU string
	var currentSGBD string
	var currentJob *ECUKomJob
	var elementStack []string

	for {
		tok, err := decoder.Token()
		if err != nil {
			break
		}

		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			elementStack = append(elementStack, name)

			attrs := attrMap(t.Attr)

			switch {
			case isECUElement(name):
				currentECU = firstNonEmpty(
					attrs["variant"], attrs["ecuvariant"],
					attrs["name"], attrs["ecu_sgbd"],
					attrs["title_ecutree"], attrs["ecutitle"],
				)
				currentSGBD = firstNonEmpty(attrs["sgbd"], attrs["ecu_sgbd"], attrs["ecugroup"])

			case isJobElement(name):
				currentJob = &ECUKomJob{
					ECU:     currentECU,
					SGBD:    currentSGBD,
					JobName: firstNonEmpty(attrs["name"], attrs["jobname"], attrs["job"]),
				}

			case isResultElement(name) && currentJob != nil:
				r := ECUKomResult{
					Name:   firstNonEmpty(attrs["name"], attrs["resultname"]),
					Value:  firstNonEmpty(attrs["value"], attrs["resultvalue"]),
					Format: firstNonEmpty(attrs["format"], attrs["type"]),
				}
				if r.Name != "" {
					currentJob.Results = append(currentJob.Results, r)
				}
			}

		case xml.EndElement:
			name := strings.ToLower(t.Name.Local)

			if isJobElement(name) && currentJob != nil {
				if currentJob.JobName != "" {
					result.Jobs = append(result.Jobs, *currentJob)
				}
				currentJob = nil
			}
			if isECUElement(name) {
				currentECU = ""
				currentSGBD = ""
			}

			if len(elementStack) > 0 {
				elementStack = elementStack[:len(elementStack)-1]
			}

		case xml.CharData:
			if currentJob != nil && len(elementStack) > 0 {
				parent := elementStack[len(elementStack)-1]
				text := strings.TrimSpace(string(t))
				if text == "" {
					continue
				}
				switch {
				case strings.Contains(parent, "jobname") || parent == "name" && isJobContext(elementStack):
					if currentJob.JobName == "" {
						currentJob.JobName = text
					}
				case strings.Contains(parent, "sgbd"):
					if currentJob.SGBD == "" {
						currentJob.SGBD = text
					}
					if currentECU == "" {
						currentECU = text
					}
				case strings.Contains(parent, "variant") || strings.Contains(parent, "ecuvariant"):
					if currentECU == "" {
						currentECU = text
					}
				}
			}
		}
	}

	if len(result.Jobs) == 0 {
		return nil, fmt.Errorf("no EDIABAS jobs found in ECUKom XML")
	}
	return result, nil
}

func isECUElement(name string) bool {
	return name == "ecu" || name == "ecuentry" || name == "ecujob" ||
		name == "ecukom" || name == "ecuvariant"
}

func isJobElement(name string) bool {
	return name == "job" || name == "ecujob" || name == "diagjob" ||
		name == "ecujobeintrag" || name == "jobentry"
}

func isResultElement(name string) bool {
	return name == "result" || name == "jobresult" || name == "diagresult" ||
		name == "resultentry" || name == "ergebnis"
}

func isJobContext(stack []string) bool {
	for _, s := range stack {
		if isJobElement(s) {
			return true
		}
	}
	return false
}

func attrMap(attrs []xml.Attr) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		m[strings.ToLower(a.Name.Local)] = a.Value
	}
	return m
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

var logLineRe = regexp.MustCompile(
	`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2}:\d{2}[\.,]\d{3})\s+` +
		`\[?\s*(\w+)\s*\]?\s+` +
		`(?:\[([^\]]*)\]\s+)?` +
		`(.+)$`,
)

var keyEventPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)session\s+(start|end|begin|finish|close|open)`),
	regexp.MustCompile(`(?i)vehicle\s+(connect|disconnect|identif|found|lost)`),
	regexp.MustCompile(`(?i)(ECU|ecu)\s+(ident|read|scan|connect|diagnos)`),
	regexp.MustCompile(`(?i)fault\s+(code|read|clear|delete|store)`),
	regexp.MustCompile(`(?i)(DTC|dtc)\s+(read|clear|found|count)`),
	regexp.MustCompile(`(?i)(FASTA|fasta|test)\s+(start|end|result|pass|fail|execut)`),
	regexp.MustCompile(`(?i)programm(ing|ed|start|end|flash)`),
	regexp.MustCompile(`(?i)(error|exception|fail|timeout|abort)`),
	regexp.MustCompile(`(?i)VIN\s*[:=]?\s*[A-HJ-NPR-Z0-9]{17}`),
	regexp.MustCompile(`(?i)I-?Level|integration.?level`),
	regexp.MustCompile(`(?i)battery|voltage|ignition`),
	regexp.MustCompile(`(?i)ENET|OBD|ICOM|DoIP|connect`),
}

func parseIstaOperationLog(data []byte) []TimelineEvent {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	var events []TimelineEvent
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 20 {
			continue
		}

		m := logLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}

		ts, level, source, msg := m[1], strings.ToUpper(m[2]), m[3], m[4]

		if level == "DEBUG" || level == "TRACE" {
			continue
		}

		if !isKeyEvent(msg) && level != "ERROR" && level != "WARN" {
			continue
		}

		events = append(events, TimelineEvent{
			Timestamp: ts,
			Level:     level,
			Source:    source,
			Message:   truncateString(msg, 200),
		})
	}

	return events
}

func isKeyEvent(msg string) bool {
	for _, re := range keyEventPatterns {
		if re.MatchString(msg) {
			return true
		}
	}
	return false
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
