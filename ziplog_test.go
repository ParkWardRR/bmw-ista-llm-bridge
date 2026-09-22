package main

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestIsECUKomFile(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// Positive cases: VIN-based XML files
		{"WBAXXXXXXXX00000_F32.xml", true},
		{"ecu_data.xml", true},
		{"DiagResult.XML", true},
		{"some_file.xml", true},

		// Negative: not .xml
		{"IstaOperation.log", false},
		{"readme.txt", false},
		{"data.json", false},

		// Negative: starts with RG_
		{"RG_TRANS_xxx.xml", false},
		{"RG_META_data.xml", false},
		{"rg_something.xml", false},

		// Negative: starts with [
		{"[Content_Types].xml", false},

		// Negative: contains behdat
		{"behdat_log.xml", false},
		{"session_BEHDAT.xml", false},
		{"my_behdat_file.xml", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isECUKomFile(tt.name)
			if got != tt.want {
				t.Errorf("isECUKomFile(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestIsKeyEvent(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		// Positive: session events
		{"session started", true},
		{"Session Start detected", true},
		{"session ended normally", true},

		// Positive: vehicle events
		{"vehicle connected via OBD", true},
		{"Vehicle identification complete", true},
		{"vehicle disconnected", true},

		// Positive: ECU events
		{"ECU identification complete", true},
		{"ECU read operation started", true},
		{"ecu scan in progress", true},

		// Positive: fault code events
		{"fault codes read successfully", true},
		{"Fault code cleared", true},

		// Positive: DTC events
		{"DTC read completed", true},
		{"dtc clear operation", true},

		// Positive: FASTA events
		{"FASTA test started", true},
		{"fasta result recorded", true},

		// Positive: programming events
		{"programming started for ECU", true},
		{"flash programmed successfully", true},

		// Positive: error events
		{"error occurred during read", true},
		{"exception in handler", true},
		{"operation failed", true},
		{"connection timeout detected", true},

		// Positive: VIN
		{"VIN: WBAXXXXXXXXX00001", true},
		{"VIN=WBA12345678901234", true},

		// Positive: I-Level
		{"I-Level detected: F020-25-03-530", true},
		{"integration level check", true},

		// Positive: battery/voltage
		{"battery voltage low", true},
		{"ignition state changed", true},

		// Positive: connection
		{"ENET connection established", true},
		{"OBD port detected", true},
		{"ICOM adapter found", true},
		{"DoIP connection ready", true},
		{"connect to vehicle", true},

		// Negative: random messages
		{"random log message", false},
		{"processing data buffer", false},
		{"cache entry updated", false},
		{"configuration loaded", false},
		{"rendering UI component", false},
	}

	for _, tt := range tests {
		t.Run(tt.msg, func(t *testing.T) {
			got := isKeyEvent(tt.msg)
			if got != tt.want {
				t.Errorf("isKeyEvent(%q) = %v, want %v", tt.msg, got, tt.want)
			}
		})
	}
}

func TestParseIstaOperationLog(t *testing.T) {
	t.Run("extracts key events skips DEBUG and TRACE", func(t *testing.T) {
		log := strings.Join([]string{
			"2025-03-15 09:22:30,123 [INFO] [SessionMgr] session started",
			"2025-03-15 09:22:30,200 [DEBUG] [Internal] allocating buffer",
			"2025-03-15 09:22:31,000 [TRACE] [Net] packet received",
			"2025-03-15 09:22:32,456 [INFO] [VehicleComm] vehicle connected via ENET",
			"2025-03-15 09:22:33,789 [INFO] [DiagMgr] ECU identification complete",
		}, "\n")

		events := parseIstaOperationLog([]byte(log))
		if len(events) != 3 {
			t.Fatalf("got %d events, want 3 (DEBUG and TRACE should be skipped)", len(events))
		}

		if events[0].Message != "session started" {
			t.Errorf("events[0].Message = %q, want %q", events[0].Message, "session started")
		}
		if events[0].Timestamp != "2025-03-15 09:22:30,123" {
			t.Errorf("events[0].Timestamp = %q, want %q", events[0].Timestamp, "2025-03-15 09:22:30,123")
		}
		if events[0].Level != "INFO" {
			t.Errorf("events[0].Level = %q, want %q", events[0].Level, "INFO")
		}
		if events[0].Source != "SessionMgr" {
			t.Errorf("events[0].Source = %q, want %q", events[0].Source, "SessionMgr")
		}
	})

	t.Run("ERROR and WARN lines always included", func(t *testing.T) {
		log := strings.Join([]string{
			"2025-03-15 09:22:30,123 [ERROR] [Core] something broke internally",
			"2025-03-15 09:22:31,000 [WARN] [Core] disk space running low",
			"2025-03-15 09:22:32,000 [INFO] [Core] normal non-key message",
		}, "\n")

		events := parseIstaOperationLog([]byte(log))
		if len(events) != 2 {
			t.Fatalf("got %d events, want 2 (ERROR and WARN always included, non-key INFO skipped)", len(events))
		}
		if events[0].Level != "ERROR" {
			t.Errorf("events[0].Level = %q, want %q", events[0].Level, "ERROR")
		}
		if events[1].Level != "WARN" {
			t.Errorf("events[1].Level = %q, want %q", events[1].Level, "WARN")
		}
	})

	t.Run("message truncation at 200 chars", func(t *testing.T) {
		longMsg := strings.Repeat("x", 250)
		log := "2025-03-15 09:22:30,123 [ERROR] [Core] " + longMsg

		events := parseIstaOperationLog([]byte(log))
		if len(events) != 1 {
			t.Fatalf("got %d events, want 1", len(events))
		}
		if len(events[0].Message) != 200 {
			t.Errorf("message length = %d, want 200", len(events[0].Message))
		}
		if !strings.HasSuffix(events[0].Message, "...") {
			t.Error("truncated message should end with '...'")
		}
	})

	t.Run("short lines are skipped", func(t *testing.T) {
		log := "short\n2025-03-15 09:22:30,123 [ERROR] [Core] real error here"

		events := parseIstaOperationLog([]byte(log))
		if len(events) != 1 {
			t.Fatalf("got %d events, want 1", len(events))
		}
	})

	t.Run("empty input returns nil", func(t *testing.T) {
		events := parseIstaOperationLog([]byte(""))
		if len(events) != 0 {
			t.Errorf("got %d events, want 0", len(events))
		}
	})
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{"short string unchanged", "hello", 10, "hello"},
		{"exact length unchanged", "hello", 5, "hello"},
		{"long string truncated", "hello world", 8, "hello..."},
		{"very long string", strings.Repeat("a", 300), 200, strings.Repeat("a", 197) + "..."},
		{"empty string", "", 10, ""},
		{"maxLen equals 3", "abcdef", 3, "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateString(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncateString(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
			if len(got) > tt.maxLen {
				t.Errorf("result length %d exceeds maxLen %d", len(got), tt.maxLen)
			}
		})
	}
}

func TestAttrMap(t *testing.T) {
	t.Run("converts to lowercase keys", func(t *testing.T) {
		attrs := []xml.Attr{
			{Name: xml.Name{Local: "Name"}, Value: "Test1"},
			{Name: xml.Name{Local: "ID"}, Value: "abc"},
			{Name: xml.Name{Local: "Result"}, Value: "PASSED"},
		}

		m := attrMap(attrs)

		if m["name"] != "Test1" {
			t.Errorf("m[\"name\"] = %q, want %q", m["name"], "Test1")
		}
		if m["id"] != "abc" {
			t.Errorf("m[\"id\"] = %q, want %q", m["id"], "abc")
		}
		if m["result"] != "PASSED" {
			t.Errorf("m[\"result\"] = %q, want %q", m["result"], "PASSED")
		}
	})

	t.Run("preserves values as-is", func(t *testing.T) {
		attrs := []xml.Attr{
			{Name: xml.Name{Local: "value"}, Value: "Mixed Case Value"},
		}

		m := attrMap(attrs)
		if m["value"] != "Mixed Case Value" {
			t.Errorf("value not preserved: got %q", m["value"])
		}
	})

	t.Run("empty slice returns empty map", func(t *testing.T) {
		m := attrMap(nil)
		if len(m) != 0 {
			t.Errorf("expected empty map, got %d entries", len(m))
		}
	})
}

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		name string
		vals []string
		want string
	}{
		{"first is non-empty", []string{"a", "b", "c"}, "a"},
		{"first empty second non-empty", []string{"", "b", "c"}, "b"},
		{"all empty returns empty", []string{"", "", ""}, ""},
		{"no arguments returns empty", nil, ""},
		{"single non-empty", []string{"only"}, "only"},
		{"single empty", []string{""}, ""},
		{"skips multiple empties", []string{"", "", "", "found"}, "found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstNonEmpty(tt.vals...)
			if got != tt.want {
				t.Errorf("firstNonEmpty(%v) = %q, want %q", tt.vals, got, tt.want)
			}
		})
	}
}
