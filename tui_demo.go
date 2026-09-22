//go:build !windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Styles
// ---------------------------------------------------------------------------

var (
	bmwBlue  = lipgloss.Color("#1C69D4")
	clrWhite = lipgloss.Color("#FFFFFF")
	clrGray  = lipgloss.Color("#888888")
	clrDim   = lipgloss.Color("#555555")
	clrGreen = lipgloss.Color("#00C853")
	clrRed   = lipgloss.Color("#FF5252")
	clrAmber = lipgloss.Color("#FFB300")

	styleBanner = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(bmwBlue).
			Padding(0, 3)

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

	styleWarn = lipgloss.NewStyle().
			Foreground(clrAmber)

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

	styleToken = lipgloss.NewStyle().
			Foreground(clrAmber).
			Bold(true)

	styleSelected = lipgloss.NewStyle().
			Foreground(bmwBlue).
			Bold(true)

	styleCheck = lipgloss.NewStyle().
			Foreground(clrGreen)

	styleUncheck = lipgloss.NewStyle().
			Foreground(clrDim)
)

// ---------------------------------------------------------------------------
// Views
// ---------------------------------------------------------------------------

type tuiView int

const (
	viewBridge   tuiView = iota // main dashboard — sources + gather
	viewGather                  // progress animation
	viewContext                 // compiled context with sections
	viewSessions                // session browser
	viewTools                   // secondary tools menu
	viewLive                    // ENET diagnostics
	viewImport                  // data import
)

// ---------------------------------------------------------------------------
// Messages
// ---------------------------------------------------------------------------

type gatherDoneMsg struct {
	context  string
	sections []contextSection
	summary  string
	tokens   int
	faults   int
	active   int
	ecus     int
	passed   int
	failed   int
	sources  []string
	err      string
}

type contextSection struct {
	key     string
	label   string
	tokens  int
	content string
	include bool
}

type liveResultMsg struct {
	mode   int
	ecus   []map[string]interface{}
	faults []map[string]interface{}
	vin    string
	err    string
}

type importResultMsg struct {
	result string
	files  []string
	err    string
}

type statusFlashMsg struct {
	text string
	ok   bool
}

type clearFlashMsg struct{}

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

type tuiModel struct {
	view   tuiView
	width  int
	height int

	sp    spinner.Model
	input textinput.Model
	tbl   table.Model

	// Data sources (discovered at init)
	sessions   []Session
	hasEnet    bool
	hasImport  bool
	hasContext bool
	hasDB      bool

	// Gather state
	gathering  bool
	gatherStep int
	gatherMsg  string

	// Context state
	contextReady bool
	contextText  string
	sections     []contextSection
	sectionIdx   int
	tokens       int
	summary      string
	faultCount   int
	activeFaults int
	ecuCount     int
	testsPassed  int
	testsFailed  int
	sources      []string
	contextErr   string

	// Live state
	liveMode    int
	liveRunning bool
	liveDone    bool
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

	// Scroll for context preview
	scrollY int

	// Status flash
	statusMsg string
	statusOK  bool
	savedPath string

	// Print-and-quit
	printAndQuit bool
}

func newTUIModel() tuiModel {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(bmwBlue)

	ti := textinput.New()
	ti.CharLimit = 255
	ti.Width = 50

	return tuiModel{
		view:       viewBridge,
		sp:         sp,
		input:      ti,
		sessions:   mockSessions(),
		hasEnet:    true,
		hasImport:  true,
		hasContext: true,
		hasDB:      true,
	}
}

// ---------------------------------------------------------------------------
// Mock data
// ---------------------------------------------------------------------------

func mockSessions() []Session {
	return []Session{
		{VIN: "WBAPH5C55BA123456", Model: "F22", Timestamp: time.Date(2026, 9, 21, 14, 30, 0, 0, time.Local),
			TransFile: "RG_TRANS.xml", ZipLog: "session.zip.log", BehDat: "slot1.behdat", FstDat: "slot1.fstdat"},
		{VIN: "WBA3N9C50FK123789", Model: "F30", Timestamp: time.Date(2026, 9, 18, 9, 15, 0, 0, time.Local),
			TransFile: "RG_TRANS.xml", ZipLog: "session.zip.log"},
		{VIN: "WBAXXXXXXXX99001", Model: "G20", Timestamp: time.Date(2026, 9, 10, 16, 45, 0, 0, time.Local),
			TransFile: "RG_TRANS.xml"},
	}
}

func mockGatherResult() gatherDoneMsg {
	vehicleSec := "# BMW F22 228i N20/AUT — WBAPH5C55BA123456\n2014 | 45,230 km | I-Level: F020-15-11-502 | via ENET | ECE\n"
	faultsSec := `## 6 Faults (2 active, 4 stored)
ACTIVE  DME    0048029A  P0300   Random/multiple cylinder misfire detected
ACTIVE  DSC    00480246  —       ABS wheel speed sensor front left: signal implausible
STORED  DME    004812A0  P0171   System too lean, bank 1
STORED  DSC    00D16334  —       Steering angle sensor: not calibrated
STORED  FEM    002A1710  —       Terminal 30g: undervoltage
STORED  KOMBI  00123456  —       Check Control: engine oil level low
`
	ecusSec := `## ECUs: 3/38 with faults
DME    KCAN   Bosch      MEVD17.2.G  2 fault(s)
DSC    KCAN   Continental MK100       2 fault(s)
FEM    BCAN   Lear       FEM_20_V11  1 fault(s)
[+35 ECUs — all OK]
`
	ecusFullSec := `## All 38 ECUs
DME    KCAN   Bosch      MEVD17.2.G       SW:SWFL_00008680958_001  2 fault(s)
DSC    KCAN   Continental MK100            SW:SWFL_34526879867_001  2 fault(s)
FEM    BCAN   Lear       FEM_20_V11       SW:SWFL_61359389610_001  1 fault(s)
EGS    KCAN   ZF         8HP45Z           SW:SWFL_00001090250_001
KOMBI  KCAN   Continental IC_HIGH_V4      SW:SWFL_62109395493_001
ACSM   KCAN   Autoliv    ACSM5_V2        SW:SWFL_34529337508_001
HU_NBT MOST   Harman     NBT_EVO_V5      SW:SWFL_63509412345_001
IHKA   KCAN   Behr       IHKA4_BMW       SW:SWFL_64119876543_001
[+30 more — all OK]
`
	testsSec := `## Tests: 10 passed, 2 failed
FAIL  Brake pad wear — front pads 2.1mm (min 3mm)
FAIL  Battery capacity — 58% (min 70%)
PASS  Alternator output — 14.2V
PASS  Coolant level — OK
PASS  Engine oil level — OK
[+7 more passed]
`
	timelineSec := `## Timeline (28 events)
14:30:00  Vehicle connected via ENET
14:30:05  Identified F22 228i WBAPH5C55BA123456
14:30:12  DME: fault memory read — 2 faults
14:30:14  DSC: fault memory read — 2 faults
14:30:15  FEM: fault memory read — 1 fault
14:30:20  KOMBI: fault memory read — 1 fault
14:30:45  FASTA started — 12 tests queued
14:31:20  FAIL Brake pad wear
14:31:35  FAIL Battery capacity
14:32:00  Session complete — 38 ECUs, 6 faults, 12 tests
`

	full := vehicleSec + "\n" + faultsSec + "\n" + ecusSec + "\n" + testsSec + "\n" + timelineSec
	fullExpanded := vehicleSec + "\n" + faultsSec + "\n" + ecusFullSec + "\n" + testsSec + "\n" + timelineSec

	_ = fullExpanded

	return gatherDoneMsg{
		context: full,
		tokens:  len(full) / 4,
		summary: "F22 228i WBAPH5C55BA123456",
		faults:  6, active: 2, ecus: 38, passed: 10, failed: 2,
		sources: []string{"session", "faults", "ecus", "tests", "timeline"},
		sections: []contextSection{
			{key: "vehicle", label: "Vehicle", tokens: len(vehicleSec) / 4, content: vehicleSec, include: true},
			{key: "faults", label: "Faults (6)", tokens: len(faultsSec) / 4, content: faultsSec, include: true},
			{key: "ecus", label: "ECUs (faults only)", tokens: len(ecusSec) / 4, content: ecusSec, include: true},
			{key: "ecus-full", label: "ECUs (all 38)", tokens: len(ecusFullSec) / 4, content: ecusFullSec, include: false},
			{key: "tests", label: "Tests (12)", tokens: len(testsSec) / 4, content: testsSec, include: true},
			{key: "timeline", label: "Timeline (28)", tokens: len(timelineSec) / 4, content: timelineSec, include: true},
		},
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func (m tuiModel) Init() tea.Cmd {
	return m.sp.Tick
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

	case gatherDoneMsg:
		m.gathering = false
		if msg.err != "" {
			m.contextErr = msg.err
			m.view = viewBridge
			return m, nil
		}
		m.contextReady = true
		m.contextText = msg.context
		m.sections = msg.sections
		m.tokens = msg.tokens
		m.summary = msg.summary
		m.faultCount = msg.faults
		m.activeFaults = msg.active
		m.ecuCount = msg.ecus
		m.testsPassed = msg.passed
		m.testsFailed = msg.failed
		m.sources = msg.sources
		m.sectionIdx = 0
		m.scrollY = 0
		m.view = viewContext
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

	case statusFlashMsg:
		m.statusMsg = msg.text
		m.statusOK = msg.ok
		return m, tea.Tick(3*time.Second, func(time.Time) tea.Msg { return clearFlashMsg{} })

	case clearFlashMsg:
		m.statusMsg = ""
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	if m.view == viewImport {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}

	switch m.view {
	case viewBridge:
		return m.keyBridge(msg)
	case viewGather:
		return m, nil // no input during gather
	case viewContext:
		return m.keyContext(msg)
	case viewSessions:
		return m.keySessions(msg)
	case viewTools:
		return m.keyTools(msg)
	case viewLive:
		return m.keyLive(msg)
	case viewImport:
		return m.keyImport(msg)
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Key: Bridge dashboard
// ---------------------------------------------------------------------------

func (m tuiModel) keyBridge(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "g":
		m.gathering = true
		m.gatherStep = 0
		m.contextErr = ""
		m.view = viewGather
		return m, tea.Batch(m.sp.Tick, m.doGather())
	case "s":
		m.view = viewSessions
		m.tbl = buildSessionTable(m.sessions)
		return m, nil
	case "t":
		m.view = viewTools
		return m, nil
	case "q":
		return m, tea.Quit
	}
	return m, nil
}

func (m tuiModel) doGather() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(800 * time.Millisecond)
		return mockGatherResult()
	}
}

// ---------------------------------------------------------------------------
// Key: Context view
// ---------------------------------------------------------------------------

func (m tuiModel) keyContext(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.view = viewBridge
		return m, nil
	case tea.KeyUp:
		if m.sectionIdx > 0 {
			m.sectionIdx--
		}
		return m, nil
	case tea.KeyDown:
		if m.sectionIdx < len(m.sections)-1 {
			m.sectionIdx++
		}
		return m, nil
	}

	switch msg.String() {
	case " ":
		if m.sectionIdx < len(m.sections) {
			m.sections[m.sectionIdx].include = !m.sections[m.sectionIdx].include

			// ECU mutual exclusivity: faults-only vs full
			cur := m.sections[m.sectionIdx]
			if cur.key == "ecus" && cur.include {
				for i := range m.sections {
					if m.sections[i].key == "ecus-full" {
						m.sections[i].include = false
					}
				}
			}
			if cur.key == "ecus-full" && cur.include {
				for i := range m.sections {
					if m.sections[i].key == "ecus" {
						m.sections[i].include = false
					}
				}
			}
			m.rebuildContext()
		}
		return m, nil
	case "c":
		if err := clipboard.WriteAll(m.contextText); err != nil {
			return m, func() tea.Msg {
				return statusFlashMsg{
					text: fmt.Sprintf("Clipboard failed: %v — use [w] to save or [p] to print", err),
					ok:   false,
				}
			}
		}
		return m, func() tea.Msg {
			return statusFlashMsg{
				text: fmt.Sprintf("Copied to clipboard (~%d tokens)", m.tokens),
				ok:   true,
			}
		}
	case "w":
		outPath := filepath.Join(".", "context.txt")
		if err := os.WriteFile(outPath, []byte(m.contextText), 0644); err != nil {
			return m, func() tea.Msg {
				return statusFlashMsg{text: fmt.Sprintf("Write failed: %v", err), ok: false}
			}
		}
		abs, _ := filepath.Abs(outPath)
		m.savedPath = abs
		return m, func() tea.Msg {
			return statusFlashMsg{
				text: fmt.Sprintf("Saved to %s (~%d tokens)", abs, m.tokens),
				ok:   true,
			}
		}
	case "p":
		m.printAndQuit = true
		return m, tea.Quit
	case "enter":
		m.view = viewBridge
		return m, nil
	}
	return m, nil
}

func (m *tuiModel) rebuildContext() {
	var parts []string
	for _, s := range m.sections {
		if s.include && len(s.content) > 0 {
			parts = append(parts, s.content)
		}
	}
	m.contextText = strings.Join(parts, "\n")
	m.tokens = len(m.contextText) / 4
}

// ---------------------------------------------------------------------------
// Key: Sessions
// ---------------------------------------------------------------------------

func (m tuiModel) keySessions(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		m.view = viewBridge
		return m, nil
	}
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

// ---------------------------------------------------------------------------
// Key: Tools
// ---------------------------------------------------------------------------

func (m tuiModel) keyTools(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.view = viewBridge
		return m, nil
	}
	switch msg.String() {
	case "l":
		m.view = viewLive
		m.liveMode = 0
		m.liveDone = false
		m.liveRunning = false
		m.liveErr = ""
		return m, nil
	case "i":
		m.view = viewImport
		m.impDone = false
		m.impRunning = false
		m.impErr = ""
		m.input.Placeholder = "Path to diagnostic scan JSON"
		m.input.Reset()
		m.input.Focus()
		return m, textinput.Blink
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// Key: Live ENET
// ---------------------------------------------------------------------------

var liveModes = []struct{ key, label string }{
	{"1", "Faults"}, {"2", "ECUs"}, {"3", "VIN"},
}

func (m tuiModel) keyLive(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		if !m.liveRunning {
			m.view = viewTools
		}
		return m, nil
	case tea.KeyEnter:
		if m.liveRunning {
			return m, nil
		}
		m.liveRunning = true
		m.liveDone = false
		mode := m.liveMode
		return m, tea.Batch(m.sp.Tick, func() tea.Msg {
			time.Sleep(700 * time.Millisecond)
			switch mode {
			case 0:
				return liveResultMsg{mode: 0, faults: mockLiveFaults()}
			case 1:
				return liveResultMsg{mode: 1, ecus: mockLiveEcus()}
			case 2:
				return liveResultMsg{mode: 2, vin: "WBAPH5C55BA123456"}
			}
			return liveResultMsg{err: "unknown mode"}
		})
	case tea.KeyTab:
		m.liveMode = (m.liveMode + 1) % len(liveModes)
		m.liveDone = false
		return m, nil
	}
	for i, mode := range liveModes {
		if msg.String() == mode.key {
			m.liveMode = i
			m.liveDone = false
			return m, nil
		}
	}
	return m, nil
}

func mockLiveFaults() []map[string]interface{} {
	return []map[string]interface{}{
		{"dtc_code": "48029A", "status_text": "testFailed,confirmedDTC", "present": "true", "stored": "true"},
		{"dtc_code": "480246", "status_text": "confirmedDTC", "present": "false", "stored": "true"},
		{"dtc_code": "D16334", "status_text": "confirmedDTC", "present": "false", "stored": "true"},
		{"dtc_code": "2A1710", "status_text": "pendingDTC", "present": "false", "stored": "false"},
	}
}

func mockLiveEcus() []map[string]interface{} {
	return []map[string]interface{}{
		{"address_hex": "0x00", "vin": "WBAPH5C55BA123456", "hw_version": "BOSCH_0261S18842", "sw_version": "8680958", "supplier": "Bosch"},
		{"address_hex": "0x12", "hw_version": "1090095", "sw_version": "1090250", "supplier": "ZF"},
		{"address_hex": "0x18", "hw_version": "34526862014", "sw_version": "34526879867", "supplier": "Continental"},
		{"address_hex": "0x21", "hw_version": "FEM_20_V11", "sw_version": "61359389610", "supplier": "Lear"},
		{"address_hex": "0x30", "hw_version": "62109383573", "sw_version": "62109395493", "supplier": "Continental"},
		{"address_hex": "0x56", "hw_version": "ACSM5_V2", "sw_version": "34529337508", "supplier": "Autoliv"},
	}
}

// ---------------------------------------------------------------------------
// Key: Import
// ---------------------------------------------------------------------------

func (m tuiModel) keyImport(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		if !m.impRunning {
			m.view = viewTools
			m.input.Blur()
		}
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
		return m, tea.Batch(m.sp.Tick, func() tea.Msg {
			time.Sleep(500 * time.Millisecond)
			return importResultMsg{
				result: "bmweb | VIN: WBA3N9C50FK123789 | 38 ECUs | 5 faults",
				files:  []string{"vehicle.json", "faults.json", "ecus.json", "import_meta.json"},
			}
		})
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
	case viewBridge:
		content = m.viewBridge()
	case viewGather:
		content = m.viewGathering()
	case viewContext:
		content = m.viewContextReady()
	case viewSessions:
		content = m.viewSessionList()
	case viewTools:
		content = m.viewToolsMenu()
	case viewLive:
		content = m.viewLiveEnet()
	case viewImport:
		content = m.viewImportData()
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(content)
}

// ---------------------------------------------------------------------------
// View: Bridge dashboard
// ---------------------------------------------------------------------------

func (m tuiModel) viewBridge() string {
	var b strings.Builder

	banner := styleBanner.Render(
		styleTitle.Render("BMW ISTA → LLM Bridge") + "\n" +
			styleSubtitle.Render("Gather everything. Ask your LLM."))
	b.WriteString(banner + "\n\n")

	// Data sources
	b.WriteString(styleSection.Render("Sources") + "\n\n")

	writeSource := func(name, detail string, ok bool) {
		status := styleOK.Render("✓")
		if !ok {
			status = styleFail.Render("✗")
		}
		b.WriteString(fmt.Sprintf("  %s  %s  %s\n", status, styleLabel.Render(name), styleSubtitle.Render(detail)))
	}

	writeSource("ISTA Sessions", fmt.Sprintf("%d found", len(m.sessions)), len(m.sessions) > 0)
	writeSource("DiagDocDb", "232 tables, 7.9M VIN ranges", m.hasDB)
	writeSource("ista-enet", "read-only ENET/HSFZ", m.hasEnet)
	writeSource("ista-import", "BMWeb/Beemuu/svietlik", m.hasImport)
	writeSource("ista-context", "compact LLM renderer (Nim)", m.hasContext)

	// Latest session
	if len(m.sessions) > 0 {
		s := m.sessions[0]
		b.WriteString("\n" + styleSection.Render("Latest Session") + "\n\n")
		b.WriteString(fmt.Sprintf("  %s  %s  %s\n",
			styleNeutral.Render(s.Model),
			styleNeutral.Render(s.VIN),
			styleSubtitle.Render(s.Timestamp.Format("2006-01-02 15:04"))))

		var files []string
		if s.TransFile != "" {
			files = append(files, "XML")
		}
		if s.ZipLog != "" {
			files = append(files, "logs")
		}
		if s.BehDat != "" || s.FstDat != "" {
			files = append(files, "FASTA")
		}
		b.WriteString(fmt.Sprintf("  %s\n", styleSubtitle.Render("Data: "+strings.Join(files, ", "))))
	}

	if m.contextErr != "" {
		b.WriteString("\n" + styleError.Render("Error: "+m.contextErr) + "\n")
	}

	// Stats from last context if available
	if m.contextReady {
		b.WriteString("\n" + styleSep.Render(strings.Repeat("─", 52)) + "\n")
		b.WriteString(styleSubtitle.Render("  Last context: "))
		b.WriteString(styleToken.Render(fmt.Sprintf("~%d tokens", m.tokens)))
		b.WriteString(styleSubtitle.Render(fmt.Sprintf(" | %d faults (%d active) | %d ECUs\n", m.faultCount, m.activeFaults, m.ecuCount)))
	}

	// Actions
	b.WriteString("\n" + styleSep.Render(strings.Repeat("─", 52)) + "\n\n")

	b.WriteString(styleMenuKey.Render("[Enter]") + " " + styleNeutral.Render("Gather All → Build LLM Context") + "\n")
	b.WriteString(styleMenuKey.Render("[s]") + "     " + styleMenuDesc.Render("Browse sessions") + "    ")
	b.WriteString(styleMenuKey.Render("[t]") + " " + styleMenuDesc.Render("Tools (ENET, import)") + "\n")
	b.WriteString(styleMenuKey.Render("[q]") + "     " + styleMenuDesc.Render("Quit") + "\n")

	return b.String()
}

// ---------------------------------------------------------------------------
// View: Gathering progress
// ---------------------------------------------------------------------------

func (m tuiModel) viewGathering() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Gathering diagnostic data...") + "\n\n")

	steps := []struct{ name, detail string }{
		{"ISTA Session", "parsing XML, correlating files"},
		{"Fault Codes", "reading DTCs from all ECUs"},
		{"ECU Data", "versions, suppliers, bus topology"},
		{"FASTA Tests", "test results, behavioral data"},
		{"Timeline", "session events from IstaOperation.log"},
		{"Rendering", "ista-context (Nim) → compact output"},
	}

	for i, step := range steps {
		if i < 5 {
			b.WriteString(styleOK.Render("  ✓ ") + styleNeutral.Render(step.name) +
				styleSubtitle.Render("  "+step.detail) + "\n")
		} else {
			b.WriteString("  " + m.sp.View() + " " + styleNeutral.Render(step.name) +
				styleSubtitle.Render("  "+step.detail) + "\n")
		}
	}

	b.WriteString("\n" + styleHelp.Render("Building token-efficient context for your LLM..."))
	return b.String()
}

// ---------------------------------------------------------------------------
// View: Context ready
// ---------------------------------------------------------------------------

func (m tuiModel) viewContextReady() string {
	var b strings.Builder

	// Header with token count
	totalTokens := 0
	for _, s := range m.sections {
		if s.include {
			totalTokens += s.tokens
		}
	}

	sizeKB := float64(len(m.contextText)) / 1024.0

	header := styleBanner.Render(
		styleTitle.Render("Context Ready") + "\n" +
			styleToken.Render(fmt.Sprintf("~%d tokens", totalTokens)) +
			styleSubtitle.Render(fmt.Sprintf(" · %.1f KB · %d sources", sizeKB, len(m.sources))))
	b.WriteString(header + "\n\n")

	// Stats row
	b.WriteString("  ")
	if m.activeFaults > 0 {
		b.WriteString(styleFail.Render(fmt.Sprintf("%d active", m.activeFaults)) + "  ")
	}
	b.WriteString(styleNeutral.Render(fmt.Sprintf("%d faults", m.faultCount)) + "  ")
	b.WriteString(styleNeutral.Render(fmt.Sprintf("%d ECUs", m.ecuCount)) + "  ")
	if m.testsFailed > 0 {
		b.WriteString(styleFail.Render(fmt.Sprintf("%d tests failed", m.testsFailed)) + "  ")
	}
	if m.testsPassed > 0 {
		b.WriteString(styleOK.Render(fmt.Sprintf("%d passed", m.testsPassed)))
	}
	b.WriteString("\n\n")

	// Section toggles
	b.WriteString(styleSection.Render("Sections") + "\n\n")

	for i, s := range m.sections {
		cursor := "  "
		if i == m.sectionIdx {
			cursor = styleSelected.Render("▶ ")
		}
		check := styleUncheck.Render("[ ]")
		if s.include {
			check = styleCheck.Render("[✓]")
		}
		tokenStr := styleSubtitle.Render(fmt.Sprintf("%4d tok", s.tokens))
		label := styleNeutral.Render(s.label)
		if i == m.sectionIdx {
			label = styleSelected.Render(s.label)
		}
		b.WriteString(fmt.Sprintf("%s%s  %-28s  %s\n", cursor, check, label, tokenStr))
	}

	b.WriteString(styleSep.Render("  "+strings.Repeat("─", 46)) + "\n")
	b.WriteString(fmt.Sprintf("       %-28s  %s\n",
		styleNeutral.Render("Total"),
		styleToken.Render(fmt.Sprintf("%4d tok", totalTokens))))

	// Preview
	b.WriteString("\n" + styleSection.Render("Preview") + "\n\n")

	lines := strings.Split(m.contextText, "\n")
	maxPreview := 12
	if m.height > 40 {
		maxPreview = 20
	}
	start := m.scrollY
	end := start + maxPreview
	if end > len(lines) {
		end = len(lines)
	}
	if start > len(lines) {
		start = len(lines)
	}

	for _, line := range lines[start:end] {
		if strings.HasPrefix(line, "# ") {
			b.WriteString(styleTitle.Render("  "+line) + "\n")
		} else if strings.HasPrefix(line, "## ") {
			b.WriteString(styleSection.Render("  "+line) + "\n")
		} else if strings.HasPrefix(line, "ACTIVE") {
			b.WriteString(styleFail.Render("  "+line) + "\n")
		} else if strings.HasPrefix(line, "FAIL") {
			b.WriteString(styleFail.Render("  "+line) + "\n")
		} else if strings.HasPrefix(line, "STORED") {
			b.WriteString(styleWarn.Render("  "+line) + "\n")
		} else if strings.HasPrefix(line, "PASS") {
			b.WriteString(styleOK.Render("  "+line) + "\n")
		} else {
			b.WriteString(styleSubtitle.Render("  "+line) + "\n")
		}
	}

	if end < len(lines) {
		b.WriteString(styleSubtitle.Render(fmt.Sprintf("  ... %d more lines", len(lines)-end)) + "\n")
	}

	// Status flash
	if m.statusMsg != "" {
		if m.statusOK {
			b.WriteString("\n" + styleOK.Render("  ✓ "+m.statusMsg) + "\n")
		} else {
			b.WriteString("\n" + styleError.Render("  ✗ "+m.statusMsg) + "\n")
		}
	}

	if m.savedPath != "" && m.statusMsg == "" {
		b.WriteString("\n" + styleSubtitle.Render("  Last saved: "+m.savedPath) + "\n")
	}

	// Actions
	b.WriteString("\n")
	b.WriteString(styleMenuKey.Render("[c]") + " " + styleNeutral.Render("Copy to clipboard") + "    ")
	b.WriteString(styleMenuKey.Render("[w]") + " " + styleMenuDesc.Render("Save context.txt") + "    ")
	b.WriteString(styleMenuKey.Render("[p]") + " " + styleMenuDesc.Render("Print to stdout") + "\n")
	b.WriteString(styleMenuKey.Render("[Space]") + " " + styleMenuDesc.Render("Toggle section") + "  ")
	b.WriteString(styleMenuKey.Render("[↑↓]") + " " + styleMenuDesc.Render("Navigate") + "  ")
	b.WriteString(styleMenuKey.Render("[Esc]") + " " + styleMenuDesc.Render("Back") + "\n")

	return b.String()
}

// ---------------------------------------------------------------------------
// View: Sessions
// ---------------------------------------------------------------------------

func (m tuiModel) viewSessionList() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render("Sessions") +
		styleSubtitle.Render(fmt.Sprintf("  %d found", len(m.sessions))) + "\n\n")
	b.WriteString(m.tbl.View() + "\n\n")
	b.WriteString(styleHelp.Render("Up/Down: Navigate   Enter: Gather this session   Esc: Back"))
	return b.String()
}

// ---------------------------------------------------------------------------
// View: Tools
// ---------------------------------------------------------------------------

func (m tuiModel) viewToolsMenu() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Tools") +
		styleSubtitle.Render("  secondary diagnostics") + "\n\n")

	type mi struct{ k, d, note string }
	tools := []mi{
		{"l", "Live ENET Diagnostics", "read-only — faults, ECUs, VIN from car"},
		{"i", "Import Scan Data", "BMWeb, Beemuu, svietlik, klartext"},
	}

	for _, t := range tools {
		b.WriteString(styleMenuKey.Render("["+t.k+"]") + " " +
			styleNeutral.Render(t.d) + "\n")
		b.WriteString("    " + styleSubtitle.Render(t.note) + "\n\n")
	}

	b.WriteString(styleHelp.Render("Esc: Back to bridge"))
	return b.String()
}

// ---------------------------------------------------------------------------
// View: Live ENET
// ---------------------------------------------------------------------------

func (m tuiModel) viewLiveEnet() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Live Diagnostics") +
		styleSubtitle.Render("  ista-enet (Nim, read-only)") + "\n\n")

	b.WriteString(styleFail.Render("SAFETY: Read-only. Writes blocked at protocol layer.") + "\n")
	b.WriteString(styleFail.Render("        Will NOT connect if ISTA is running.") + "\n")
	b.WriteString(styleFail.Render("        Cannot interfere with any running session.") + "\n\n")

	for i, mode := range liveModes {
		if i == m.liveMode {
			b.WriteString(styleMenuKey.Render("["+mode.key+"]") + " " + styleOK.Render(mode.label) + "  ")
		} else {
			b.WriteString(styleMenuKey.Render("["+mode.key+"]") + " " + styleMenuDesc.Render(mode.label) + "  ")
		}
	}
	b.WriteString("\n\n")

	if m.liveRunning {
		b.WriteString(m.sp.View() + " Connecting to vehicle...\n")
		return b.String()
	}

	if m.liveErr != "" {
		b.WriteString(styleError.Render("Error: "+m.liveErr) + "\n")
	}

	if m.liveDone {
		switch m.liveMode {
		case 0:
			for _, f := range m.liveFaults {
				code := fmtVal(f, "dtc_code")
				status := fmtVal(f, "status_text")
				present := fmtVal(f, "present")
				marker, ms := "[STORED]", styleSubtitle
				if present == "true" {
					marker, ms = "[ACTIVE]", styleFail
				}
				b.WriteString(ms.Render(marker) + " " + styleNeutral.Render("0x"+code) + " " + styleSubtitle.Render(status) + "\n")
			}
		case 1:
			for _, e := range m.liveEcus {
				addr := fmtVal(e, "address_hex")
				hw := fmtVal(e, "hw_version")
				supplier := fmtVal(e, "supplier")
				b.WriteString(styleNeutral.Render(addr) + "  " + styleNeutral.Render(supplier) + "  " + styleSubtitle.Render(hw) + "\n")
			}
		case 2:
			if m.liveVIN != "" {
				b.WriteString(styleBox.Render(styleLabel.Render("VIN")+styleNeutral.Render(m.liveVIN)) + "\n")
			}
		}
	}

	if !m.liveDone {
		b.WriteString(styleHelp.Render("Enter: Run   Tab/1-3: Mode   Esc: Back"))
	} else {
		b.WriteString("\n" + styleHelp.Render("Tab/1-3: Switch mode   Esc: Back"))
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// View: Import
// ---------------------------------------------------------------------------

func (m tuiModel) viewImportData() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Import Diagnostic Data") +
		styleSubtitle.Render("  ista-import (Nim)") + "\n\n")

	b.WriteString(styleSubtitle.Render("Auto-detects BMWeb, Beemuu, svietlik, klartext from JSON structure.") + "\n\n")
	b.WriteString(styleLabel.Render("File") + m.input.View() + "\n\n")

	if m.impRunning {
		b.WriteString(m.sp.View() + " Importing...\n")
	} else if m.impErr != "" {
		b.WriteString(styleError.Render("Error: "+m.impErr) + "\n")
	} else if m.impDone {
		b.WriteString(styleOK.Render("Import complete!") + "\n")
		if m.impResult != "" {
			b.WriteString(styleSubtitle.Render(m.impResult) + "\n\n")
		}
		for _, f := range m.impFiles {
			b.WriteString(styleOK.Render("  + ") + styleNeutral.Render(f) + "\n")
		}
	}

	b.WriteString("\n" + styleHelp.Render("Enter: Import   Esc: Back"))
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

func buildSessionTable(sessions []Session) table.Model {
	cols := []table.Column{
		{Title: "Date", Width: 18},
		{Title: "VIN", Width: 19},
		{Title: "Model", Width: 8},
		{Title: "XML", Width: 5},
		{Title: "Logs", Width: 5},
		{Title: "FASTA", Width: 6},
	}

	rows := make([]table.Row, len(sessions))
	for i, s := range sessions {
		x, l, f := " -", " -", "  -"
		if s.TransFile != "" {
			x = " +"
		}
		if s.ZipLog != "" {
			l = " +"
		}
		if s.BehDat != "" || s.FstDat != "" {
			f = "  +"
		}
		mdl := s.Model
		if mdl == "" {
			mdl = "--"
		}
		rows[i] = table.Row{s.Timestamp.Format("2006-01-02 15:04"), s.VIN, mdl, x, l, f}
	}

	h := len(sessions)
	if h > 12 {
		h = 12
	}
	if h < 1 {
		h = 1
	}

	t := table.New(table.WithColumns(cols), table.WithRows(rows), table.WithFocused(true), table.WithHeight(h))
	ts := table.DefaultStyles()
	ts.Header = ts.Header.BorderStyle(lipgloss.NormalBorder()).BorderForeground(bmwBlue).BorderBottom(true).Bold(true).Foreground(bmwBlue)
	ts.Selected = ts.Selected.Foreground(lipgloss.Color("#FFFFFF")).Background(bmwBlue).Bold(false)
	t.SetStyles(ts)
	return t
}

// Suppress unused import warnings
var _ = json.Marshal

func runTUI() {
	p := tea.NewProgram(newTUIModel(), tea.WithAltScreen())
	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}

	if m, ok := result.(tuiModel); ok && m.printAndQuit && m.contextText != "" {
		fmt.Print(m.contextText)
		fmt.Fprintf(os.Stderr, "\n---\n~%d tokens | pipe to clipboard: ... | pbcopy (macOS) or ... | clip (Windows)\n", m.tokens)
	}
}
