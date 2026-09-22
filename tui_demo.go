//go:build !windows

package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Styles (same as tui.go)
// ---------------------------------------------------------------------------

var (
	bmwBlue  = lipgloss.Color("#1C69D4")
	clrWhite = lipgloss.Color("#FFFFFF")
	clrGray  = lipgloss.Color("#888888")
	clrDim   = lipgloss.Color("#555555")
	clrGreen = lipgloss.Color("#00C853")
	clrRed   = lipgloss.Color("#FF5252")

	styleBanner = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(bmwBlue).
			Padding(1, 3)

	styleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(clrWhite)

	styleSubtitle = lipgloss.NewStyle().
			Foreground(clrGray)

	styleSection = lipgloss.NewStyle().
			Bold(true).
			Foreground(bmwBlue).
			MarginTop(1)

	styleLabel = lipgloss.NewStyle().
			Foreground(clrGray).
			Width(20)

	styleOK = lipgloss.NewStyle().
		Foreground(clrGreen)

	styleFail = lipgloss.NewStyle().
			Foreground(clrRed)

	styleNeutral = lipgloss.NewStyle().
			Foreground(clrWhite)

	styleMenuKey = lipgloss.NewStyle().
			Foreground(bmwBlue).
			Bold(true)

	styleMenuDesc = lipgloss.NewStyle().
			Foreground(clrGray)

	styleSep = lipgloss.NewStyle().
			Foreground(clrDim)

	styleBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(bmwBlue).
			Padding(0, 2)

	styleHelp = lipgloss.NewStyle().
			Foreground(clrDim).
			MarginTop(1)

	styleError = lipgloss.NewStyle().
			Foreground(clrRed).
			Bold(true)
)

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

type initResultMsg struct {
	sessions []Session
	stats    captureStats
}

type captureStats struct {
	totalScreenshots int
	totalSize        int64
	lastCaptureTime  time.Time
	sessionCount     int
}

type queryResultMsg struct {
	label   string
	query   string
	results []map[string]interface{}
	err     string
}

type liveResultMsg struct {
	mode   int
	ecus   []map[string]interface{}
	faults []map[string]interface{}
	vin    string
	err    string
}

type importResultMsg struct {
	outDir string
	files  []string
	result string
	err    string
}

type tickMsg time.Time

// ---------------------------------------------------------------------------
// Views
// ---------------------------------------------------------------------------

type tuiView int

const (
	viewDashboard tuiView = iota
	viewSessions
	viewVIN
	viewLookup
	viewLive
	viewImport
)

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

type tuiModel struct {
	view   tuiView
	width  int
	height int

	loading  bool
	sessions []Session
	stats    captureStats

	sp    spinner.Model
	input textinput.Model
	tbl   table.Model

	// VIN/Lookup state
	qLoading   bool
	qResults   []map[string]interface{}
	qLabel     string
	qQuery     string
	qErr       string
	lookupMode int

	// Live state
	liveRunning bool
	liveDone    bool
	liveMode    int
	liveEcus    []map[string]interface{}
	liveFaults  []map[string]interface{}
	liveVIN     string
	liveErr     string

	// Import state
	impRunning bool
	impDone    bool
	impFiles   []string
	impResult  string
	impErr     string
}

func newTUIModel() tuiModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(bmwBlue)

	ti := textinput.New()
	ti.CharLimit = 40
	ti.Width = 36

	return tuiModel{
		view:    viewDashboard,
		loading: true,
		sp:      sp,
		input:   ti,
	}
}

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

func mockSessions() []Session {
	return []Session{
		{
			VIN:       "WBAPH5C55BA123456",
			Model:     "F22",
			Timestamp: time.Date(2026, 9, 21, 14, 30, 0, 0, time.Local),
			TransFile: "RG_TRANS_WBAPH5C55BA123456_20260921.xml",
			ZipLog:    "20260921_F22_WBAPH5C55BA123456.zip.log",
			BehDat:    "slot1_WBAPH5C55BA123456.behdat",
			FstDat:    "slot1_WBAPH5C55BA123456.fstdat",
		},
		{
			VIN:       "WBA3N9C50FK123789",
			Model:     "F30",
			Timestamp: time.Date(2026, 9, 18, 9, 15, 0, 0, time.Local),
			TransFile: "RG_TRANS_WBA3N9C50FK123789_20260918.xml",
			ZipLog:    "20260918_F30_WBA3N9C50FK123789.zip.log",
		},
		{
			VIN:       "WBAXXXXXXXX99001",
			Model:     "G20",
			Timestamp: time.Date(2026, 9, 10, 16, 45, 0, 0, time.Local),
			TransFile: "RG_TRANS_WBAXXXXXXXX99001_20260910.xml",
		},
	}
}

func mockLiveFaults() []map[string]interface{} {
	return []map[string]interface{}{
		{"dtc_code": "480246", "status_text": "confirmedDTC,testFailedSinceLastClear", "present": "false", "stored": "true"},
		{"dtc_code": "48029A", "status_text": "testFailed,confirmedDTC,warningIndicatorRequested", "present": "true", "stored": "true"},
		{"dtc_code": "D16334", "status_text": "confirmedDTC", "present": "false", "stored": "true"},
		{"dtc_code": "2A1710", "status_text": "pendingDTC", "present": "false", "stored": "false"},
	}
}

func mockLiveEcus() []map[string]interface{} {
	return []map[string]interface{}{
		{"address_hex": "0x00", "vin": "WBAPH5C55BA123456", "hw_version": "BOSCH_0261S18842", "sw_version": "8680958", "supplier": "Bosch"},
		{"address_hex": "0x12", "vin": "WBAPH5C55BA123456", "hw_version": "1090095", "sw_version": "1090250", "supplier": "ZF"},
		{"address_hex": "0x18", "vin": "", "hw_version": "34526862014", "sw_version": "34526879867", "supplier": "Continental"},
		{"address_hex": "0x21", "vin": "", "hw_version": "FEM_20_V11", "sw_version": "61359389610", "supplier": "Lear"},
		{"address_hex": "0x30", "vin": "", "hw_version": "62109383573", "sw_version": "62109395493", "supplier": "Continental"},
		{"address_hex": "0x56", "vin": "", "hw_version": "ACSM5_V2", "sw_version": "34529337508", "supplier": "Autoliv"},
	}
}

func mockVINResults() []map[string]interface{} {
	return []map[string]interface{}{
		{"TYPSCHLUESSEL": "1N71", "VIN17_4_7": "PH5C", "VINBANDFROM": "A000001", "VINBANDTO": "A999999", "PRODUCTIONDATEYEAR": "2014", "PRODUCTIONDATEMONTH": "03", "GEARBOX_TYPE": "AUT"},
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(m.sp.Tick, func() tea.Msg {
		time.Sleep(600 * time.Millisecond)
		return initResultMsg{
			sessions: mockSessions(),
			stats: captureStats{
				totalScreenshots: 142,
				totalSize:        4_821_504,
				lastCaptureTime:  time.Date(2026, 9, 21, 14, 52, 0, 0, time.Local),
				sessionCount:     3,
			},
		}
	})
}

// ---------------------------------------------------------------------------
// Update
// ---------------------------------------------------------------------------

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.sp, cmd = m.sp.Update(msg)
		return m, cmd

	case initResultMsg:
		m.loading = false
		m.sessions = msg.sessions
		m.stats = msg.stats
		return m, nil

	case queryResultMsg:
		m.qLoading = false
		m.qLabel = msg.label
		m.qQuery = msg.query
		m.qResults = msg.results
		m.qErr = msg.err
		return m, nil

	case liveResultMsg:
		m.liveRunning = false
		m.liveDone = true
		m.liveEcus = msg.ecus
		m.liveFaults = msg.faults
		m.liveVIN = msg.vin
		m.liveErr = msg.err
		return m, nil

	case importResultMsg:
		m.impRunning = false
		m.impDone = true
		m.impFiles = msg.files
		m.impResult = msg.result
		m.impErr = msg.err
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.view == viewVIN || m.view == viewLookup || m.view == viewImport {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// Key handling
// ---------------------------------------------------------------------------

func (m tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}

	switch m.view {
	case viewDashboard:
		return m.keyDashboard(msg)
	case viewSessions:
		return m.keySessions(msg)
	case viewVIN:
		return m.keyVIN(msg)
	case viewLookup:
		return m.keyLookup(msg)
	case viewLive:
		return m.keyLive(msg)
	case viewImport:
		return m.keyImport(msg)
	}
	return m, nil
}

func (m tuiModel) keyDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.loading {
		return m, nil
	}
	switch msg.String() {
	case "s":
		m.view = viewSessions
		m.tbl = buildTable(m.sessions)
		return m, nil
	case "v":
		m.view = viewVIN
		m.qResults = nil
		m.qErr = ""
		m.qLoading = false
		m.input.Placeholder = "Enter VIN (17 chars or 4-7 model code)"
		m.input.CharLimit = 17
		m.input.Reset()
		m.input.Focus()
		return m, textinput.Blink
	case "f":
		m.view = viewLookup
		m.lookupMode = 0
		m.qResults = nil
		m.qErr = ""
		m.qLoading = false
		m.input.Placeholder = "Enter P-code (e.g. P0300)"
		m.input.CharLimit = 40
		m.input.Reset()
		m.input.Focus()
		return m, textinput.Blink
	case "l":
		m.view = viewLive
		m.liveMode = 0
		m.liveDone = false
		m.liveRunning = false
		m.liveErr = ""
		m.liveEcus = nil
		m.liveFaults = nil
		m.liveVIN = ""
		return m, nil
	case "i":
		m.view = viewImport
		m.impDone = false
		m.impRunning = false
		m.impErr = ""
		m.impFiles = nil
		m.impResult = ""
		m.input.Placeholder = "Path to scan file (BMWeb/Beemuu/svietlik JSON)"
		m.input.CharLimit = 255
		m.input.Reset()
		m.input.Focus()
		return m, textinput.Blink
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m tuiModel) keySessions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.view = viewDashboard
		return m, nil
	}
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m tuiModel) keyVIN(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.view = viewDashboard
		m.input.Blur()
		return m, nil
	case tea.KeyEnter:
		if m.qLoading {
			return m, nil
		}
		query := strings.TrimSpace(m.input.Value())
		if query == "" {
			return m, nil
		}
		m.qLoading = true
		m.qResults = nil
		m.qErr = ""
		return m, tea.Batch(m.sp.Tick, func() tea.Msg {
			time.Sleep(300 * time.Millisecond)
			return queryResultMsg{
				label:   "VIN",
				query:   query,
				results: mockVINResults(),
			}
		})
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

var lookupModes = []struct {
	key, label, placeholder string
}{
	{"1", "P-Code", "Enter P-code (e.g. P0300)"},
	{"2", "Fault Code", "Enter BMW fault code"},
	{"3", "Check Control", "Enter CC message text"},
	{"4", "Diag Code", "Enter diagnostic code"},
}

func (m tuiModel) keyLookup(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.view = viewDashboard
		m.input.Blur()
		return m, nil
	case tea.KeyTab:
		m.lookupMode = (m.lookupMode + 1) % len(lookupModes)
		m.input.Placeholder = lookupModes[m.lookupMode].placeholder
		m.qResults = nil
		m.qErr = ""
		return m, nil
	}
	k := msg.String()
	for i, mode := range lookupModes {
		if k == mode.key && len(m.input.Value()) == 0 {
			m.lookupMode = i
			m.input.Placeholder = mode.placeholder
			m.qResults = nil
			m.qErr = ""
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

var liveModes = []struct {
	key, label string
}{
	{"1", "Faults"},
	{"2", "ECUs"},
	{"3", "VIN"},
}

func (m tuiModel) keyLive(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		if m.liveRunning {
			return m, nil
		}
		m.view = viewDashboard
		return m, nil
	case tea.KeyEnter:
		if m.liveRunning {
			return m, nil
		}
		m.liveRunning = true
		m.liveDone = false
		m.liveErr = ""
		mode := m.liveMode
		return m, tea.Batch(m.sp.Tick, func() tea.Msg {
			time.Sleep(800 * time.Millisecond)
			switch mode {
			case 0:
				return liveResultMsg{mode: 0, faults: mockLiveFaults()}
			case 1:
				return liveResultMsg{mode: 1, ecus: mockLiveEcus()}
			case 2:
				return liveResultMsg{mode: 2, vin: "WBAPH5C55BA123456"}
			}
			return liveResultMsg{mode: mode, err: "unknown mode"}
		})
	case tea.KeyTab:
		m.liveMode = (m.liveMode + 1) % len(liveModes)
		m.liveDone = false
		m.liveErr = ""
		return m, nil
	}

	k := msg.String()
	for i, mode := range liveModes {
		if k == mode.key {
			m.liveMode = i
			m.liveDone = false
			m.liveErr = ""
			return m, nil
		}
	}

	if m.liveDone || m.liveErr != "" {
		m.view = viewDashboard
		m.liveDone = false
		m.liveErr = ""
		return m, nil
	}
	return m, nil
}

func (m tuiModel) keyImport(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		if m.impRunning {
			return m, nil
		}
		m.view = viewDashboard
		m.input.Blur()
		return m, nil
	case tea.KeyEnter:
		if m.impRunning {
			return m, nil
		}
		path := strings.TrimSpace(m.input.Value())
		if path == "" {
			return m, nil
		}
		m.impRunning = true
		m.impDone = false
		m.impErr = ""
		return m, tea.Batch(m.sp.Tick, func() tea.Msg {
			time.Sleep(500 * time.Millisecond)
			return importResultMsg{
				result: "Source: bmweb  VIN: WBA3N9C50FK123789  ECUs: 38  Faults: 5",
				files:  []string{"vehicle.json", "faults.json", "ecus.json", "import_meta.json"},
			}
		})
	}

	if m.impDone || m.impErr != "" {
		if msg.String() != "" {
			m.impDone = false
			m.impErr = ""
			m.input.Reset()
			m.input.Focus()
			return m, textinput.Blink
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m tuiModel) View() string {
	var content string
	switch m.view {
	case viewDashboard:
		content = m.viewDashboard()
	case viewSessions:
		content = m.viewSessions()
	case viewVIN:
		content = m.viewVINLookup()
	case viewLookup:
		content = m.viewFaultLookup()
	case viewLive:
		content = m.viewLive()
	case viewImport:
		content = m.viewImportData()
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(content)
}

func (m tuiModel) viewDashboard() string {
	var b strings.Builder

	banner := styleBanner.Render(
		styleTitle.Render("BMW ISTA LLM Bridge") + "\n" +
			styleSubtitle.Render("Capture · Diagnostics · Database Lookup"))
	b.WriteString(banner + "\n\n")

	if m.loading {
		b.WriteString(m.sp.View() + " Initializing...\n")
		return b.String()
	}

	b.WriteString(styleSection.Render("System Status") + "\n\n")

	b.WriteString(styleLabel.Render("ISTA Install") +
		styleOK.Render("Found") +
		styleSubtitle.Render("  C:\\EC-APPS\\ISTA") + "\n")

	b.WriteString(styleLabel.Render("Encoder") +
		styleOK.Render("libaom-av1  av1  AVIF") + "\n")

	if len(m.sessions) > 0 {
		info := fmt.Sprintf("%d discovered", len(m.sessions))
		latest := m.sessions[0].Timestamp.Format("2006-01-02")
		b.WriteString(styleLabel.Render("Sessions") +
			styleNeutral.Render(info) +
			styleSubtitle.Render("  latest "+latest) + "\n")
	}

	b.WriteString(styleLabel.Render("DiagDocDb") +
		styleOK.Render("Connected  232 tables") + "\n")

	b.WriteString(styleLabel.Render("ista-enet") +
		styleOK.Render("Available") +
		styleSubtitle.Render("  read-only ENET/HSFZ") + "\n")

	b.WriteString(styleLabel.Render("ista-import") +
		styleOK.Render("Available") +
		styleSubtitle.Render("  BMWeb/Beemuu/svietlik") + "\n")

	if m.stats.totalScreenshots > 0 {
		b.WriteString("\n" + styleSection.Render("Capture Stats") + "\n\n")
		b.WriteString(styleLabel.Render("Screenshots") +
			styleNeutral.Render(fmt.Sprintf("%d", m.stats.totalScreenshots)) + "\n")
		b.WriteString(styleLabel.Render("Total Size") +
			styleNeutral.Render(fmtBytes(m.stats.totalSize)) + "\n")
		b.WriteString(styleLabel.Render("Capture Sessions") +
			styleNeutral.Render(fmt.Sprintf("%d", m.stats.sessionCount)) + "\n")
		if !m.stats.lastCaptureTime.IsZero() {
			b.WriteString(styleLabel.Render("Last Capture") +
				styleNeutral.Render(m.stats.lastCaptureTime.Format("2006-01-02 15:04")) + "\n")
		}
	}

	b.WriteString("\n" + styleSep.Render(strings.Repeat("-", 56)) + "\n\n")

	type mi struct{ k, d string }
	row1 := []mi{{"w", "Watch"}, {"s", "Sessions"}, {"b", "Bundle latest"}}
	row2 := []mi{{"v", "VIN Lookup"}, {"f", "Fault Code"}, {"r", "Report"}}
	row3 := []mi{{"l", "Live (ENET)"}, {"i", "Import"}}

	render := func(items []mi) string {
		parts := make([]string, len(items))
		for i, it := range items {
			parts[i] = styleMenuKey.Render("["+it.k+"]") + " " + styleMenuDesc.Render(it.d)
		}
		return strings.Join(parts, "   ")
	}

	b.WriteString(render(row1) + "\n")
	b.WriteString(render(row2) + "\n")
	b.WriteString(render(row3) + "\n")
	b.WriteString(styleMenuKey.Render("[q]") + " " + styleMenuDesc.Render("Quit") + "\n")

	return b.String()
}

func (m tuiModel) viewSessions() string {
	var b strings.Builder

	header := styleTitle.Render("Sessions")
	if len(m.sessions) > 0 {
		header += styleSubtitle.Render(fmt.Sprintf("  %d found", len(m.sessions)))
	}
	b.WriteString(header + "\n\n")
	b.WriteString(m.tbl.View() + "\n")
	b.WriteString("\n")
	b.WriteString(styleHelp.Render("Up/Down: Navigate   Enter: Bundle selected   Esc: Back"))
	return b.String()
}

func (m tuiModel) viewVINLookup() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("VIN Lookup") +
		styleSubtitle.Render("  DiagDocDb VINRANGES") + "\n\n")

	b.WriteString(styleLabel.Render("VIN") + m.input.View() + "\n\n")

	if m.qLoading {
		b.WriteString(m.sp.View() + " Querying database...\n")
		return b.String()
	}

	if m.qResults != nil {
		if len(m.qResults) == 0 {
			b.WriteString(styleSubtitle.Render("No matching VIN ranges found.") + "\n")
		} else {
			b.WriteString(styleSection.Render(
				fmt.Sprintf("Found %d match(es)", len(m.qResults))) + "\n\n")
			for _, r := range m.qResults {
				var lines []string
				if v := fmtVal(r, "TYPSCHLUESSEL"); v != "" {
					lines = append(lines, styleLabel.Render("Type Key")+styleNeutral.Render(v))
				}
				if v := fmtVal(r, "VIN17_4_7"); v != "" {
					lines = append(lines, styleLabel.Render("VIN 4-7")+styleNeutral.Render(v))
				}
				from := fmtVal(r, "VINBANDFROM")
				to := fmtVal(r, "VINBANDTO")
				if from != "" || to != "" {
					lines = append(lines, styleLabel.Render("Range")+
						styleNeutral.Render(from+" - "+to))
				}
				yr := fmtVal(r, "PRODUCTIONDATEYEAR")
				mo := fmtVal(r, "PRODUCTIONDATEMONTH")
				if yr != "" {
					lines = append(lines, styleLabel.Render("Production")+
						styleNeutral.Render(mo+"/"+yr))
				}
				if v := fmtVal(r, "GEARBOX_TYPE"); v != "" {
					lines = append(lines, styleLabel.Render("Gearbox")+styleNeutral.Render(v))
				}
				b.WriteString(styleBox.Render(strings.Join(lines, "\n")) + "\n")
			}
		}
	}

	b.WriteString("\n" + styleHelp.Render("Enter: Search   Esc: Back"))
	return b.String()
}

func (m tuiModel) viewFaultLookup() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Fault Code Lookup") +
		styleSubtitle.Render("  DiagDocDb") + "\n\n")

	for i, mode := range lookupModes {
		if i == m.lookupMode {
			b.WriteString(styleMenuKey.Render("["+mode.key+"]") + " " +
				styleOK.Render(mode.label) + "  ")
		} else {
			b.WriteString(styleMenuKey.Render("["+mode.key+"]") + " " +
				styleMenuDesc.Render(mode.label) + "  ")
		}
	}
	b.WriteString("\n\n")

	b.WriteString(styleLabel.Render("Search") + m.input.View() + "\n\n")

	b.WriteString(styleHelp.Render("Enter: Search   Tab: Mode   1-4: Select mode   Esc: Back"))
	return b.String()
}

func (m tuiModel) viewLive() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Live Diagnostics") +
		styleSubtitle.Render("  ista-enet (read-only)") + "\n\n")

	b.WriteString(styleFail.Render("SAFETY: Read-only mode — write operations are blocked at the protocol layer") + "\n\n")

	for i, mode := range liveModes {
		if i == m.liveMode {
			b.WriteString(styleMenuKey.Render("["+mode.key+"]") + " " +
				styleOK.Render(mode.label) + "  ")
		} else {
			b.WriteString(styleMenuKey.Render("["+mode.key+"]") + " " +
				styleMenuDesc.Render(mode.label) + "  ")
		}
	}
	b.WriteString("\n\n")

	if m.liveRunning {
		b.WriteString(m.sp.View() + " Connecting to vehicle via ENET...\n")
		return b.String()
	}

	if m.liveErr != "" {
		b.WriteString(styleError.Render("Error: "+m.liveErr) + "\n\n")
		b.WriteString(styleHelp.Render("Press any key to return"))
		return b.String()
	}

	if m.liveDone {
		switch m.liveMode {
		case 0:
			if len(m.liveFaults) == 0 {
				b.WriteString(styleOK.Render("No faults found.") + "\n")
			} else {
				b.WriteString(styleSection.Render(
					fmt.Sprintf("Found %d fault(s)", len(m.liveFaults))) + "\n\n")
				for _, f := range m.liveFaults {
					code := fmtVal(f, "dtc_code")
					status := fmtVal(f, "status_text")
					present := fmtVal(f, "present")
					marker := "[STORED]"
					markerStyle := styleSubtitle
					if present == "true" {
						marker = "[ACTIVE]"
						markerStyle = styleFail
					}
					b.WriteString(markerStyle.Render(marker) + " " +
						styleNeutral.Render("DTC 0x"+code) + " " +
						styleSubtitle.Render(status) + "\n")
				}
			}
		case 1:
			if len(m.liveEcus) == 0 {
				b.WriteString(styleSubtitle.Render("No ECUs responded.") + "\n")
			} else {
				b.WriteString(styleSection.Render(
					fmt.Sprintf("Found %d ECU(s)", len(m.liveEcus))) + "\n\n")
				for _, e := range m.liveEcus {
					addr := fmtVal(e, "address_hex")
					vin := fmtVal(e, "vin")
					hw := fmtVal(e, "hw_version")
					sw := fmtVal(e, "sw_version")
					supplier := fmtVal(e, "supplier")

					var lines []string
					lines = append(lines, styleLabel.Render("Address")+styleNeutral.Render(addr))
					if vin != "" {
						lines = append(lines, styleLabel.Render("VIN")+styleNeutral.Render(vin))
					}
					if hw != "" {
						lines = append(lines, styleLabel.Render("HW")+styleNeutral.Render(hw))
					}
					if sw != "" {
						lines = append(lines, styleLabel.Render("SW")+styleNeutral.Render(sw))
					}
					if supplier != "" {
						lines = append(lines, styleLabel.Render("Supplier")+styleNeutral.Render(supplier))
					}
					b.WriteString(styleBox.Render(strings.Join(lines, "\n")) + "\n")
				}
			}
		case 2:
			if m.liveVIN != "" {
				b.WriteString(styleSection.Render("Vehicle Identification") + "\n\n")
				b.WriteString(styleBox.Render(
					styleLabel.Render("VIN")+styleNeutral.Render(m.liveVIN)) + "\n")
			} else {
				b.WriteString(styleSubtitle.Render("Could not read VIN from ECU.") + "\n")
			}
		}

		b.WriteString("\n" + styleHelp.Render("Press any key to return"))
		return b.String()
	}

	b.WriteString(styleHelp.Render("Enter: Run   Tab: Mode   1-3: Select mode   Esc: Back"))
	return b.String()
}

func (m tuiModel) viewImportData() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Import Diagnostic Data") +
		styleSubtitle.Render("  ista-import") + "\n\n")

	b.WriteString(styleSubtitle.Render("Import scans from BMWeb, Beemuu, svietlik, or klartext.") + "\n")
	b.WriteString(styleSubtitle.Render("Auto-detects format from JSON structure.") + "\n\n")

	b.WriteString(styleLabel.Render("File") + m.input.View() + "\n\n")

	if m.impRunning {
		b.WriteString(m.sp.View() + " Importing and normalizing...\n")
		return b.String()
	}

	if m.impErr != "" {
		b.WriteString(styleError.Render("Error: "+m.impErr) + "\n\n")
		b.WriteString(styleHelp.Render("Press any key to try again"))
		return b.String()
	}

	if m.impDone {
		b.WriteString(styleOK.Render("Import complete!") + "\n\n")
		if m.impResult != "" {
			b.WriteString(styleSubtitle.Render(m.impResult) + "\n\n")
		}
		if len(m.impFiles) > 0 {
			var fl []string
			for _, f := range m.impFiles {
				fl = append(fl, styleOK.Render("  + ")+styleNeutral.Render(f))
			}
			b.WriteString(styleBox.Render(strings.Join(fl, "\n")) + "\n\n")
		}
		b.WriteString(styleHelp.Render("Press any key to try again"))
		return b.String()
	}

	b.WriteString(styleHelp.Render("Enter: Import   Esc: Back"))
	return b.String()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func fmtVal(r map[string]interface{}, key string) string {
	v, ok := r[key]
	if !ok || v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func fmtBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

func buildTable(sessions []Session) table.Model {
	cols := []table.Column{
		{Title: "Date", Width: 18},
		{Title: "VIN", Width: 19},
		{Title: "Model", Width: 12},
		{Title: "Trans", Width: 6},
		{Title: "Log", Width: 5},
		{Title: "FASTA", Width: 7},
	}

	rows := make([]table.Row, len(sessions))
	for i, s := range sessions {
		tr := " -"
		if s.TransFile != "" {
			tr = " +"
		}
		lg := " -"
		if s.ZipLog != "" {
			lg = " +"
		}
		fa := "  -"
		if s.BehDat != "" || s.FstDat != "" {
			fa = "  +"
		}
		mdl := s.Model
		if mdl == "" {
			mdl = "--"
		}
		rows[i] = table.Row{
			s.Timestamp.Format("2006-01-02 15:04"),
			s.VIN,
			mdl,
			tr,
			lg,
			fa,
		}
	}

	h := len(sessions)
	if h > 15 {
		h = 15
	}
	if h < 1 {
		h = 1
	}

	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithFocused(true),
		table.WithHeight(h),
	)

	ts := table.DefaultStyles()
	ts.Header = ts.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(bmwBlue).
		BorderBottom(true).
		Bold(true).
		Foreground(bmwBlue)
	ts.Selected = ts.Selected.
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(bmwBlue).
		Bold(false)
	t.SetStyles(ts)

	return t
}

// Suppress unused import warnings
var _ = json.Marshal
var _ = slog.Default

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

func runTUI() {
	p := tea.NewProgram(newTUIModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}
