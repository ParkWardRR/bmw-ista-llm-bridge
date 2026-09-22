package main

import (
	"testing"
)

func TestNormalizeFASTAResult(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		// Passed variants
		{"passed", "passed"},
		{"pass", "passed"},
		{"ok", "passed"},
		{"bestanden", "passed"},
		{"true", "passed"},
		{"1", "passed"},

		// Failed variants
		{"failed", "failed"},
		{"fail", "failed"},
		{"nok", "failed"},
		{"nicht bestanden", "failed"},
		{"false", "failed"},
		{"0", "failed"},

		// Skipped variants
		{"not_executed", "skipped"},
		{"notexecuted", "skipped"},
		{"skipped", "skipped"},
		{"skip", "skipped"},
		{"nicht ausgefuehrt", "skipped"},

		// Case insensitivity
		{"PASSED", "passed"},
		{"Passed", "passed"},
		{"FAILED", "failed"},
		{"Failed", "failed"},
		{"OK", "passed"},
		{"NOK", "failed"},
		{"Bestanden", "passed"},
		{"NICHT BESTANDEN", "failed"},
		{"Not_Executed", "skipped"},
		{"SKIPPED", "skipped"},

		// Whitespace trimming
		{"  passed  ", "passed"},
		{" fail ", "failed"},
		{"\tskipped\n", "skipped"},
		{" ok ", "passed"},

		// Unknown values pass through unchanged
		{"warning", "warning"},
		{"pending", "pending"},
		{"inconclusive", "inconclusive"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeFASTAResult(tt.input)
			if got != tt.want {
				t.Errorf("normalizeFASTAResult(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseFSTATestsXML(t *testing.T) {
	t.Run("basic testresult elements", func(t *testing.T) {
		xml := `<?xml version="1.0"?>
<fasta>
  <testresult name="Brake Test" id="BT001" result="passed">
    <detail>All values within tolerance</detail>
  </testresult>
  <testresult name="Emissions Check" id="EM002" result="failed">
    <detail>CO2 above threshold</detail>
  </testresult>
</fasta>`

		tests, err := parseFSTATestsXML([]byte(xml))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(tests) != 2 {
			t.Fatalf("got %d tests, want 2", len(tests))
		}

		if tests[0].Name != "Brake Test" {
			t.Errorf("test[0].Name = %q, want %q", tests[0].Name, "Brake Test")
		}
		if tests[0].ID != "BT001" {
			t.Errorf("test[0].ID = %q, want %q", tests[0].ID, "BT001")
		}
		if tests[0].Result != "passed" {
			t.Errorf("test[0].Result = %q, want %q", tests[0].Result, "passed")
		}
		if tests[0].Detail != "All values within tolerance" {
			t.Errorf("test[0].Detail = %q, want %q", tests[0].Detail, "All values within tolerance")
		}

		if tests[1].Name != "Emissions Check" {
			t.Errorf("test[1].Name = %q, want %q", tests[1].Name, "Emissions Check")
		}
		if tests[1].Result != "failed" {
			t.Errorf("test[1].Result = %q, want %q", tests[1].Result, "failed")
		}
	})

	t.Run("teststep elements", func(t *testing.T) {
		xml := `<?xml version="1.0"?>
<fasta>
  <teststep name="Step 1" id="S1" result="ok"/>
</fasta>`

		tests, err := parseFSTATestsXML([]byte(xml))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(tests) != 1 {
			t.Fatalf("got %d tests, want 1", len(tests))
		}
		if tests[0].Result != "passed" {
			t.Errorf("result = %q, want %q (normalized from 'ok')", tests[0].Result, "passed")
		}
	})

	t.Run("test elements with child text", func(t *testing.T) {
		xml := `<?xml version="1.0"?>
<fasta>
  <test>
    <name>Headlight Alignment</name>
    <id>HL003</id>
    <result>nok</result>
    <detail>Left headlight out of range</detail>
  </test>
</fasta>`

		tests, err := parseFSTATestsXML([]byte(xml))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(tests) != 1 {
			t.Fatalf("got %d tests, want 1", len(tests))
		}
		if tests[0].Name != "Headlight Alignment" {
			t.Errorf("Name = %q, want %q", tests[0].Name, "Headlight Alignment")
		}
		if tests[0].ID != "HL003" {
			t.Errorf("ID = %q, want %q", tests[0].ID, "HL003")
		}
		if tests[0].Result != "failed" {
			t.Errorf("Result = %q, want %q (normalized from 'nok')", tests[0].Result, "failed")
		}
		if tests[0].Detail != "Left headlight out of range" {
			t.Errorf("Detail = %q, want %q", tests[0].Detail, "Left headlight out of range")
		}
	})

	t.Run("German element names pruefschritt and testergebnis", func(t *testing.T) {
		xml := `<?xml version="1.0"?>
<fasta>
  <pruefschritt name="Bremsentest" id="BT001">
    <ergebnis>bestanden</ergebnis>
    <beschreibung>Alle Werte im Toleranzbereich</beschreibung>
  </pruefschritt>
  <testergebnis name="Abgasuntersuchung" id="AU002">
    <ergebnis>nicht bestanden</ergebnis>
  </testergebnis>
</fasta>`

		tests, err := parseFSTATestsXML([]byte(xml))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(tests) != 2 {
			t.Fatalf("got %d tests, want 2", len(tests))
		}
		if tests[0].Name != "Bremsentest" {
			t.Errorf("test[0].Name = %q, want %q", tests[0].Name, "Bremsentest")
		}
		if tests[0].Result != "passed" {
			t.Errorf("test[0].Result = %q, want %q", tests[0].Result, "passed")
		}
		if tests[0].Detail != "Alle Werte im Toleranzbereich" {
			t.Errorf("test[0].Detail = %q, want %q", tests[0].Detail, "Alle Werte im Toleranzbereich")
		}
		if tests[1].Result != "failed" {
			t.Errorf("test[1].Result = %q, want %q", tests[1].Result, "failed")
		}
	})

	t.Run("result from ergebnis element with value attribute", func(t *testing.T) {
		xml := `<?xml version="1.0"?>
<fasta>
  <testresult name="Speedometer Test" id="SP001">
    <ergebnis value="not_executed"/>
  </testresult>
</fasta>`

		tests, err := parseFSTATestsXML([]byte(xml))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(tests) != 1 {
			t.Fatalf("got %d tests, want 1", len(tests))
		}
		if tests[0].Result != "skipped" {
			t.Errorf("Result = %q, want %q", tests[0].Result, "skipped")
		}
	})

	t.Run("empty XML returns error", func(t *testing.T) {
		_, err := parseFSTATestsXML([]byte(""))
		if err == nil {
			t.Fatal("expected error for empty XML, got nil")
		}
	})

	t.Run("malformed XML with no test elements returns error", func(t *testing.T) {
		xml := `<?xml version="1.0"?><root><data>hello</data></root>`
		_, err := parseFSTATestsXML([]byte(xml))
		if err == nil {
			t.Fatal("expected error for XML with no tests, got nil")
		}
	})

	t.Run("testresult with title and status attrs", func(t *testing.T) {
		xml := `<?xml version="1.0"?>
<fasta>
  <testresult title="ABS Check" testid="ABS01" status="pass"/>
</fasta>`

		tests, err := parseFSTATestsXML([]byte(xml))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(tests) != 1 {
			t.Fatalf("got %d tests, want 1", len(tests))
		}
		if tests[0].Name != "ABS Check" {
			t.Errorf("Name = %q, want %q", tests[0].Name, "ABS Check")
		}
		if tests[0].ID != "ABS01" {
			t.Errorf("ID = %q, want %q", tests[0].ID, "ABS01")
		}
		if tests[0].Result != "passed" {
			t.Errorf("Result = %q, want %q", tests[0].Result, "passed")
		}
	})
}

func TestParseFSTABehaviorXML(t *testing.T) {
	t.Run("action elements with attributes", func(t *testing.T) {
		xml := `<?xml version="1.0"?>
<behavior>
  <action type="click" target="start_button" timestamp="2025-03-15T09:22:30">
    <detail>User clicked Start</detail>
  </action>
  <action type="navigate" target="diagnostics_page" timestamp="2025-03-15T09:23:00"/>
</behavior>`

		actions, err := parseFSTABehaviorXML([]byte(xml))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(actions) != 2 {
			t.Fatalf("got %d actions, want 2", len(actions))
		}
		if actions[0].Type != "click" {
			t.Errorf("action[0].Type = %q, want %q", actions[0].Type, "click")
		}
		if actions[0].Target != "start_button" {
			t.Errorf("action[0].Target = %q, want %q", actions[0].Target, "start_button")
		}
		if actions[0].Timestamp != "2025-03-15T09:22:30" {
			t.Errorf("action[0].Timestamp = %q, want %q", actions[0].Timestamp, "2025-03-15T09:22:30")
		}
		if actions[0].Detail != "User clicked Start" {
			t.Errorf("action[0].Detail = %q, want %q", actions[0].Detail, "User clicked Start")
		}
		if actions[1].Type != "navigate" {
			t.Errorf("action[1].Type = %q, want %q", actions[1].Type, "navigate")
		}
		if actions[1].Target != "diagnostics_page" {
			t.Errorf("action[1].Target = %q, want %q", actions[1].Target, "diagnostics_page")
		}
	})

	t.Run("useraction elements", func(t *testing.T) {
		xml := `<?xml version="1.0"?>
<log>
  <useraction type="input" target="vin_field" timestamp="2025-03-15T09:24:00"/>
</log>`

		actions, err := parseFSTABehaviorXML([]byte(xml))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(actions) != 1 {
			t.Fatalf("got %d actions, want 1", len(actions))
		}
		if actions[0].Type != "input" {
			t.Errorf("Type = %q, want %q", actions[0].Type, "input")
		}
	})

	t.Run("behavior elements with child text", func(t *testing.T) {
		xml := `<?xml version="1.0"?>
<log>
  <behavior>
    <type>selection</type>
    <target>ecu_list</target>
    <timestamp>2025-03-15T09:25:00</timestamp>
    <detail>Selected DME module</detail>
  </behavior>
</log>`

		actions, err := parseFSTABehaviorXML([]byte(xml))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(actions) != 1 {
			t.Fatalf("got %d actions, want 1", len(actions))
		}
		if actions[0].Type != "selection" {
			t.Errorf("Type = %q, want %q", actions[0].Type, "selection")
		}
		if actions[0].Target != "ecu_list" {
			t.Errorf("Target = %q, want %q", actions[0].Target, "ecu_list")
		}
		if actions[0].Timestamp != "2025-03-15T09:25:00" {
			t.Errorf("Timestamp = %q, want %q", actions[0].Timestamp, "2025-03-15T09:25:00")
		}
		if actions[0].Detail != "Selected DME module" {
			t.Errorf("Detail = %q, want %q", actions[0].Detail, "Selected DME module")
		}
	})

	t.Run("empty XML returns nil actions no error", func(t *testing.T) {
		actions, err := parseFSTABehaviorXML([]byte(""))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(actions) != 0 {
			t.Errorf("got %d actions, want 0", len(actions))
		}
	})

	t.Run("action without type is skipped", func(t *testing.T) {
		xml := `<?xml version="1.0"?>
<log>
  <action target="some_target" timestamp="2025-03-15T09:26:00"/>
  <action type="click" target="ok_button" timestamp="2025-03-15T09:27:00"/>
</log>`

		actions, err := parseFSTABehaviorXML([]byte(xml))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(actions) != 1 {
			t.Fatalf("got %d actions, want 1 (action without type should be skipped)", len(actions))
		}
		if actions[0].Type != "click" {
			t.Errorf("Type = %q, want %q", actions[0].Type, "click")
		}
	})

	t.Run("alternate attribute names actiontype page time", func(t *testing.T) {
		xml := `<?xml version="1.0"?>
<log>
  <action actiontype="scroll" page="main_view" time="2025-03-15T09:28:00"/>
</log>`

		actions, err := parseFSTABehaviorXML([]byte(xml))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(actions) != 1 {
			t.Fatalf("got %d actions, want 1", len(actions))
		}
		if actions[0].Type != "scroll" {
			t.Errorf("Type = %q, want %q", actions[0].Type, "scroll")
		}
		if actions[0].Target != "main_view" {
			t.Errorf("Target = %q, want %q", actions[0].Target, "main_view")
		}
		if actions[0].Timestamp != "2025-03-15T09:28:00" {
			t.Errorf("Timestamp = %q, want %q", actions[0].Timestamp, "2025-03-15T09:28:00")
		}
	})
}
