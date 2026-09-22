package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"strings"
)

type FASTATest struct {
	Name   string `json:"name"`
	ID     string `json:"id,omitempty"`
	Result string `json:"result"`
	Detail string `json:"detail,omitempty"`
}

type FASTAAction struct {
	Type      string `json:"type"`
	Target    string `json:"target,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type FASTAData struct {
	Tests   []FASTATest   `json:"tests,omitempty"`
	Actions []FASTAAction `json:"actions,omitempty"`
}

func parseFSTATests(fstdatPath string) ([]FASTATest, error) {
	data, err := os.ReadFile(fstdatPath)
	if err != nil {
		return nil, fmt.Errorf("read fstdat: %w", err)
	}
	return parseFSTATestsXML(data)
}

func parseFSTATestsXML(data []byte) ([]FASTATest, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	var tests []FASTATest
	var current *FASTATest
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

			if isTestElement(name) {
				current = &FASTATest{
					Name:   firstNonEmpty(attrs["name"], attrs["testname"], attrs["title"]),
					ID:     firstNonEmpty(attrs["id"], attrs["testid"]),
					Result: firstNonEmpty(attrs["result"], attrs["status"]),
				}
			}

			if current != nil && isResultAttr(name) {
				if v := firstNonEmpty(attrs["value"], attrs["result"], attrs["status"]); v != "" {
					current.Result = normalizeFASTAResult(v)
				}
			}

		case xml.EndElement:
			name := strings.ToLower(t.Name.Local)

			if isTestElement(name) && current != nil {
				if current.Name != "" || current.ID != "" {
					if current.Result != "" {
						current.Result = normalizeFASTAResult(current.Result)
					}
					tests = append(tests, *current)
				}
				current = nil
			}

			if len(elementStack) > 0 {
				elementStack = elementStack[:len(elementStack)-1]
			}

		case xml.CharData:
			if current == nil || len(elementStack) == 0 {
				continue
			}
			parent := elementStack[len(elementStack)-1]
			text := strings.TrimSpace(string(t))
			if text == "" {
				continue
			}

			switch {
			case strings.Contains(parent, "name") || strings.Contains(parent, "title") || parent == "bezeichnung":
				if current.Name == "" {
					current.Name = text
				}
			case strings.Contains(parent, "result") || strings.Contains(parent, "status") || parent == "ergebnis":
				if current.Result == "" {
					current.Result = text
				}
			case strings.Contains(parent, "detail") || strings.Contains(parent, "description") || parent == "beschreibung":
				if current.Detail == "" {
					current.Detail = text
				}
			case strings.Contains(parent, "id") || parent == "testid":
				if current.ID == "" {
					current.ID = text
				}
			}
		}
	}

	if len(tests) == 0 {
		return nil, fmt.Errorf("no test results found in fstdat XML")
	}
	return tests, nil
}

func parseFSTABehavior(behdatPath string) ([]FASTAAction, error) {
	data, err := os.ReadFile(behdatPath)
	if err != nil {
		return nil, fmt.Errorf("read behdat: %w", err)
	}
	return parseFSTABehaviorXML(data)
}

func parseFSTABehaviorXML(data []byte) ([]FASTAAction, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false

	var actions []FASTAAction
	var current *FASTAAction
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

			if isActionElement(name) {
				current = &FASTAAction{
					Type:      firstNonEmpty(attrs["type"], attrs["actiontype"], attrs["action"]),
					Target:    firstNonEmpty(attrs["target"], attrs["page"], attrs["view"]),
					Timestamp: firstNonEmpty(attrs["timestamp"], attrs["time"], attrs["datetime"]),
				}
			}

		case xml.EndElement:
			name := strings.ToLower(t.Name.Local)

			if isActionElement(name) && current != nil {
				if current.Type != "" {
					actions = append(actions, *current)
				}
				current = nil
			}

			if len(elementStack) > 0 {
				elementStack = elementStack[:len(elementStack)-1]
			}

		case xml.CharData:
			if current == nil || len(elementStack) == 0 {
				continue
			}
			parent := elementStack[len(elementStack)-1]
			text := strings.TrimSpace(string(t))
			if text == "" {
				continue
			}

			switch {
			case strings.Contains(parent, "type") || parent == "actiontype":
				if current.Type == "" {
					current.Type = text
				}
			case strings.Contains(parent, "target") || strings.Contains(parent, "page"):
				if current.Target == "" {
					current.Target = text
				}
			case strings.Contains(parent, "time") || strings.Contains(parent, "timestamp"):
				if current.Timestamp == "" {
					current.Timestamp = text
				}
			case strings.Contains(parent, "detail") || strings.Contains(parent, "description"):
				if current.Detail == "" {
					current.Detail = text
				}
			}
		}
	}

	return actions, nil
}

func isTestElement(name string) bool {
	return name == "testresult" || name == "teststep" || name == "test" ||
		name == "fastatest" || name == "fstatest" || name == "testentry" ||
		name == "pruefschritt" || name == "testergebnis"
}

func isResultAttr(name string) bool {
	return name == "result" || name == "status" || name == "ergebnis" || name == "outcome"
}

func isActionElement(name string) bool {
	return name == "action" || name == "useraction" || name == "behavior" ||
		name == "entry" || name == "event" || name == "aktion" ||
		name == "sessionaction" || name == "behaviorentry"
}

func normalizeFASTAResult(r string) string {
	switch strings.ToLower(strings.TrimSpace(r)) {
	case "passed", "pass", "ok", "bestanden", "true", "1":
		return "passed"
	case "failed", "fail", "nok", "nicht bestanden", "false", "0":
		return "failed"
	case "not_executed", "notexecuted", "skipped", "skip", "nicht ausgefuehrt":
		return "skipped"
	default:
		return r
	}
}
