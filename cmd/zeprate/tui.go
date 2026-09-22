package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── Palette ────────────────────────────────────────────────────────────────

var (
	cBg      = lipgloss.Color("#0D1117")
	cSurface = lipgloss.Color("#161B22")
	cBorder  = lipgloss.Color("#30363D")
	cText    = lipgloss.Color("#C9D1D9")
	cDim     = lipgloss.Color("#484F58")
	cBlue    = lipgloss.Color("#58A6FF")
	cGreen   = lipgloss.Color("#3FB950")
	cRed     = lipgloss.Color("#F85149")
	cOrange  = lipgloss.Color("#D29922")
	cPurple  = lipgloss.Color("#BC8CFF")
	cCyan    = lipgloss.Color("#39D353")
	cWhite   = lipgloss.Color("#FFFFFF")
)

// ─── Styles ─────────────────────────────────────────────────────────────────

var (
	sTitle = lipgloss.NewStyle().Bold(true).Foreground(cWhite)
	sDim   = lipgloss.NewStyle().Foreground(cDim)
	sBlue  = lipgloss.NewStyle().Foreground(cBlue)
	sGreen = lipgloss.NewStyle().Foreground(cGreen)
	sRed   = lipgloss.NewStyle().Foreground(cRed)
	sCyan  = lipgloss.NewStyle().Foreground(cCyan)
	sKey   = lipgloss.NewStyle().Foreground(cBlue).Bold(true)

	sBox = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cBorder).
		Padding(0, 1)

	sBoxActive = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(cBlue).
			Padding(0, 1)

	sNavActive = lipgloss.NewStyle().
			Foreground(cWhite).
			Background(cBlue).
			Padding(0, 1).
			Bold(true)

	sNavInactive = lipgloss.NewStyle().
			Foreground(cDim).
			Padding(0, 1)

	sStatLabel = lipgloss.NewStyle().Foreground(cDim).Width(14)
	sStatValue = lipgloss.NewStyle().Foreground(cWhite).Bold(true)

	sStatus = lipgloss.NewStyle().Foreground(cDim)

	sHeaderCell = lipgloss.NewStyle().Foreground(cBlue).Bold(true).Padding(0, 1)
	sCell       = lipgloss.NewStyle().Foreground(cText).Padding(0, 1)
	sCellDim    = lipgloss.NewStyle().Foreground(cDim).Padding(0, 1)
	sCellAccent = lipgloss.NewStyle().Foreground(cCyan).Padding(0, 1)
)

// ─── Views ──────────────────────────────────────────────────────────────────

type view int

const (
	vDash view = iota
	vTables
	vSQL
	vVIN
	vCodes
	vExport
)

var viewNames = []struct {
	v    view
	name string
	icon string
}{
	{vDash, "Overview", "◆"},
	{vTables, "Tables", "▦"},
	{vSQL, "SQL", "▸"},
	{vVIN, "VIN", "◈"},
	{vCodes, "Codes", "◇"},
	{vExport, "Export", "↓"},
}

// ─── Messages ───────────────────────────────────────────────────────────────

type dbReadyMsg struct {
	infos []TableInfo
	err   error
}

type queryDoneMsg struct {
	rows    []map[string]interface{}
	columns []string
	err     error
	elapsed time.Duration
}

type exportDoneMsg struct {
	name string
	rows int
	err  error
}

// ─── Model ──────────────────────────────────────────────────────────────────

type model struct {
	// geometry
	w, h int

	// state
	booted   bool
	booting  bool
	bootMsg  string
	dbErr    string
	password string

	// db
	cfg    DBConfig
	db     *DiagDB
	tables []TableInfo
	total  int

	// nav
	active view

	// spinner
	sp spinner.Model

	// inputs
	sqlIn   textinput.Model
	vinIn   textinput.Model
	codeIn  textinput.Model
	filterIn textinput.Model

	// code type selector
	codeType int // 0=pcode 1=fault 2=cc 3=diag

	// sql
	sqlRows []map[string]interface{}
	sqlCols []string
	sqlTime time.Duration
	sqlErr  string
	sqlHist []string
	sqlHIdx int

	// vin
	vinRows []map[string]interface{}
	vinErr  string

	// code results
	codeRows []map[string]interface{}
	codeErr  string

	// table browser
	tblRows   []TableInfo
	tblTable  table.Model

	// export
	expLog  []string
	expDone bool

	// sizes
	sidebarW int
}

func newModel() model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(cBlue)

	sqlIn := textinput.New()
	sqlIn.Placeholder = "SELECT * FROM VINRANGES WHERE VIN17_4_7 = 'JB1C' LIMIT 10"
	sqlIn.CharLimit = 4000
	sqlIn.Width = 70

	vinIn := textinput.New()
	vinIn.Placeholder = "WBA3N3C58GF713031 or JB1C"
	vinIn.CharLimit = 17
	vinIn.Width = 30

	codeIn := textinput.New()
	codeIn.Placeholder = "P0300, misfire, etc."
	codeIn.CharLimit = 200
	codeIn.Width = 40

	filterIn := textinput.New()
	filterIn.Placeholder = "filter..."
	filterIn.CharLimit = 100
	filterIn.Width = 25

	return model{
		cfg:      defaultDBConfig(),
		sp:       sp,
		sqlIn:    sqlIn,
		vinIn:    vinIn,
		codeIn:   codeIn,
		filterIn: filterIn,
		booting:  true,
		active:   vDash,
		sidebarW: 16,
	}
}

// ─── Init ───────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	return tea.Batch(m.sp.Tick, m.boot())
}

func (m model) boot() tea.Cmd {
	return func() tea.Msg {
		pw, err := DiscoverPassword(m.cfg)
		if err != nil {
			return dbReadyMsg{err: err}
		}
		m.cfg.Password = pw
		db := NewDiagDB(m.cfg)

		infos, err := db.TableInfos(nil)
		if err != nil {
			return dbReadyMsg{err: err}
		}
		return dbReadyMsg{infos: infos}
	}
}

// ─── Update ─────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w = msg.Width
		m.h = msg.Height
		m = m.layout()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case dbReadyMsg:
		m.booting = false
		if msg.err != nil {
			m.dbErr = msg.err.Error()
			return m, nil
		}
		m.booted = true
		m.tables = msg.infos
		m.tblRows = msg.infos
		m.db = NewDiagDB(m.cfg)
		for _, t := range msg.infos {
			if t.Rows > 0 {
				m.total += t.Rows
			}
		}
		m = m.layout()
		return m, nil

	case queryDoneMsg:
		m.sqlRows = msg.rows
		m.sqlCols = msg.columns
		m.sqlTime = msg.elapsed
		if msg.err != nil {
			m.sqlErr = msg.err.Error()
		} else {
			m.sqlErr = ""
		}
		m = m.layout()
		return m, nil

	case exportDoneMsg:
		if msg.err != nil {
			m.expLog = append(m.expLog, sRed.Render(fmt.Sprintf("  ✗ %s: %v", msg.name, msg.err)))
		} else {
			m.expLog = append(m.expLog, sGreen.Render(fmt.Sprintf("  ✓ %s: %d rows", msg.name, msg.rows)))
		}
		return m, nil
	}

	// Delegate to sub-updates
	switch m.active {
	case vTables:
		return m.updateTables(msg)
	case vSQL:
		return m.updateSQL(msg)
	case vVIN:
		return m.updateVIN(msg)
	case vCodes:
		return m.updateCodes(msg)
	case vExport:
		return m.updateExport(msg)
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// If an input is focused, let the view handle it
	if m.anyFocused() {
		return m.delegateKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "1":
		m.active = vDash
	case "2":
		m.active = vTables
	case "3":
		m.active = vSQL
		m.sqlIn.Focus()
	case "4":
		m.active = vVIN
		m.vinIn.Focus()
	case "5":
		m.active = vCodes
		m.codeIn.Focus()
	case "6":
		m.active = vExport
	case "left", "h":
		if m.active > 0 {
			m.active--
		}
	case "right", "l":
		if m.active < vExport {
			m.active++
		}
	}
	return m, nil
}

func (m model) delegateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.active {
	case vTables:
		return m.updateTables(msg)
	case vSQL:
		return m.updateSQL(msg)
	case vVIN:
		return m.updateVIN(msg)
	case vCodes:
		return m.updateCodes(msg)
	case vExport:
		return m.updateExport(msg)
	}
	return m, nil
}

func (m model) anyFocused() bool {
	return m.sqlIn.Focused() || m.vinIn.Focused() || m.codeIn.Focused() || m.filterIn.Focused()
}

// ── Tables ──

func (m model) updateTables(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.filterIn.Focused() {
			switch msg.String() {
			case "enter", "esc":
				m.filterIn.Blur()
				return m, nil
			}
			var cmd tea.Cmd
			m.filterIn, cmd = m.filterIn.Update(msg)
			m.tblRows = m.filterTables()
			m = m.layout()
			return m, cmd
		}
		switch msg.String() {
		case "/", "f":
			m.filterIn.Focus()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.tblTable, cmd = m.tblTable.Update(msg)
	return m, cmd
}

func (m model) filterTables() []TableInfo {
	q := strings.ToLower(m.filterIn.Value())
	if q == "" {
		return m.tables
	}
	var out []TableInfo
	for _, t := range m.tables {
		if strings.Contains(strings.ToLower(t.Name), q) {
			out = append(out, t)
		}
	}
	return out
}

// ── SQL ──

func (m model) updateSQL(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.sqlIn.Focused() {
			switch msg.String() {
			case "enter":
				q := m.sqlIn.Value()
				if q == "" {
					return m, nil
				}
				m.sqlHist = append(m.sqlHist, q)
				m.sqlHIdx = len(m.sqlHist)
				m.sqlIn.SetValue("")
				return m, m.runSQL(q)
			case "up":
				if m.sqlHIdx > 0 {
					m.sqlHIdx--
					m.sqlIn.SetValue(m.sqlHist[m.sqlHIdx])
				}
				return m, nil
			case "down":
				if m.sqlHIdx < len(m.sqlHist)-1 {
					m.sqlHIdx++
					m.sqlIn.SetValue(m.sqlHist[m.sqlHIdx])
				} else {
					m.sqlHIdx = len(m.sqlHist)
					m.sqlIn.SetValue("")
				}
				return m, nil
			case "esc":
				m.sqlIn.Blur()
				return m, nil
			}
		}
		switch msg.String() {
		case "enter", "i":
			m.sqlIn.Focus()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.sqlIn, cmd = m.sqlIn.Update(msg)
	return m, cmd
}

func (m model) runSQL(q string) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		rows, err := m.db.query(q)
		elapsed := time.Since(start)
		var cols []string
		if len(rows) > 0 {
			for k := range rows[0] {
				cols = append(cols, k)
			}
			sort.Strings(cols)
		}
		return queryDoneMsg{rows: rows, columns: cols, err: err, elapsed: elapsed}
	}
}

// ── VIN ──

func (m model) updateVIN(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.vinIn.Focused() {
			switch msg.String() {
			case "enter":
				v := strings.TrimSpace(m.vinIn.Value())
				if v != "" {
					return m, m.lookupVIN(v)
				}
			case "esc":
				m.vinIn.Blur()
				return m, nil
			}
		}
		switch msg.String() {
		case "enter", "i":
			m.vinIn.Focus()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.vinIn, cmd = m.vinIn.Update(msg)
	return m, cmd
}

func (m model) lookupVIN(v string) tea.Cmd {
	return func() tea.Msg {
		v = strings.ToUpper(v)
		var rows []map[string]interface{}
		var err error
		if len(v) == 17 {
			rows, err = m.db.VINLookupByRange(v)
			if err == nil && len(rows) == 0 {
				rows, err = m.db.VINLookup(v[3:7])
			}
		} else if len(v) >= 4 && len(v) <= 7 {
			rows, err = m.db.VINLookup(v)
		} else {
			err = fmt.Errorf("enter 17-char VIN or positions 4-7")
		}
		return queryDoneMsg{rows: rows, err: err}
	}
}

// ── Codes ──

func (m model) updateCodes(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.codeIn.Focused() {
			switch msg.String() {
			case "enter":
				q := strings.TrimSpace(m.codeIn.Value())
				if q != "" {
					return m, m.lookupCode(q)
				}
			case "tab":
				m.codeType = (m.codeType + 1) % 4
				return m, nil
			case "esc":
				m.codeIn.Blur()
				return m, nil
			}
		}
		switch msg.String() {
		case "enter", "i":
			m.codeIn.Focus()
			return m, nil
		case "tab":
			m.codeType = (m.codeType + 1) % 4
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.codeIn, cmd = m.codeIn.Update(msg)
	return m, cmd
}

func (m model) lookupCode(q string) tea.Cmd {
	return func() tea.Msg {
		var rows []map[string]interface{}
		var err error
		switch m.codeType {
		case 0:
			rows, err = m.db.LookupPCode(q)
		case 1:
			rows, err = m.db.LookupFaultCode(q)
		case 2:
			rows, err = m.db.LookupCCMessage(q)
		case 3:
			rows, err = m.db.LookupDiagCode(q)
		}
		return queryDoneMsg{rows: rows, err: err}
	}
}

// ── Export ──

func (m model) updateExport(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter", "e":
			if !m.expDone {
				m.expDone = true
				return m, m.doExport()
			}
		}
	}
	return m, nil
}

func (m model) doExport() tea.Cmd {
	return func() tea.Msg {
		dir := "db-export"
		for _, t := range m.tables {
			if t.Rows <= 0 {
				continue
			}
			path := dir + "/" + t.Name + ".json"
			n, err := m.db.ExportTable(t.Name, path)
			if err != nil {
				// send error msg
			} else {
				_ = n
			}
		}
		return exportDoneMsg{name: "ALL", rows: len(m.tables)}
	}
}

// ─── Layout ─────────────────────────────────────────────────────────────────

func (m model) layout() model {
	// Rebuild tables table
	cols := []table.Column{
		{Title: "TABLE", Width: m.w - m.sidebarW - 25},
		{Title: "ROWS", Width: 14},
	}
	var rows []table.Row
	for _, t := range m.tblRows {
		cnt := "—"
		if t.Rows >= 0 {
			cnt = fmtNum(t.Rows)
		}
		rows = append(rows, table.Row{t.Name, cnt})
	}
	tblH := m.h - 12
	if tblH < 3 {
		tblH = 3
	}
	t := table.New(
		table.WithColumns(cols),
		table.WithRows(rows),
		table.WithHeight(tblH),
		table.WithFocused(m.active == vTables),
	)
	ts := table.DefaultStyles()
	ts.Header = sHeaderCell
	ts.Selected = lipgloss.NewStyle().Foreground(cWhite).Background(lipgloss.Color("#1F2937")).Padding(0, 1)
	t.SetStyles(ts)
	m.tblTable = t

	return m
}

// ─── View ───────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.booting {
		return m.viewBoot()
	}
	if m.dbErr != "" {
		return m.viewError()
	}

	side := m.viewSidebar()
	var content string
	switch m.active {
	case vDash:
		content = m.viewDash()
	case vTables:
		content = m.viewTables()
	case vSQL:
		content = m.viewSQL()
	case vVIN:
		content = m.viewVIN()
	case vCodes:
		content = m.viewCodes()
	case vExport:
		content = m.viewExport()
	}

	main := lipgloss.JoinVertical(lipgloss.Left,
		m.viewTopBar(),
		content,
		m.viewStatusBar(),
	)

	return lipgloss.JoinHorizontal(lipgloss.Top, side, main)
}

func (m model) viewBoot() string {
	inner := lipgloss.JoinVertical(lipgloss.Center,
		"",
		sTitle.Render("◆ zeprate"),
		sDim.Render("BMW ISTA DiagDocDb Explorer"),
		"",
		m.sp.View()+" Connecting to encrypted database...",
		"",
		sDim.Render("Discovering password from Rheingold DLLs"),
		sDim.Render("Querying 232 tables via 32-bit PowerShell bridge"),
		"",
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(cBlue).
		Padding(2, 4).
		Render(inner)

	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box)
}

func (m model) viewError() string {
	inner := lipgloss.JoinVertical(lipgloss.Center,
		"",
		sRed.Render("◆ Connection Failed"),
		"",
		sDim.Render(m.dbErr),
		"",
		sDim.Render("Make sure ISTA is installed at C:\\EC-APPS\\ISTA"),
		sDim.Render("and the TesterGUI\\bin\\Release DLLs are present."),
		"",
	)

	box := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(cRed).
		Padding(2, 4).
		Render(inner)

	return lipgloss.Place(m.w, m.h, lipgloss.Center, lipgloss.Center, box)
}

func (m model) viewTopBar() string {
	title := sTitle.Render(" zeprate ")
	sub := sDim.Render(fmt.Sprintf("%d tables • %s rows", len(m.tables), fmtNum(m.total)))
	gap := m.w - m.sidebarW - lipgloss.Width(title) - lipgloss.Width(sub) - 6
	if gap < 2 {
		gap = 2
	}
	bar := lipgloss.NewStyle().
		Background(cSurface).
		Width(m.w - m.sidebarW).
		Padding(0, 1).
		Render(title + strings.Repeat(" ", gap) + sub)
	return bar
}

func (m model) viewSidebar() string {
	var items []string
	items = append(items, "")
	items = append(items, lipgloss.NewStyle().Bold(true).Foreground(cBlue).Padding(0, 1).Render("◆ zeprate"))
	items = append(items, "")

	for _, vn := range viewNames {
		label := fmt.Sprintf(" %s %d.%s", vn.icon, vn.v+1, vn.name)
		if vn.v == m.active {
			items = append(items, sNavActive.Width(m.sidebarW-2).Render(label))
		} else {
			items = append(items, sNavInactive.Width(m.sidebarW-2).Render(label))
		}
	}

	items = append(items, "")
	items = append(items, sDim.Render(" ← → nav"))
	items = append(items, sDim.Render(" q quit"))

	side := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(cBorder).
		Width(m.sidebarW).
		Height(m.h - 2).
		Render(strings.Join(items, "\n"))

	return side
}

func (m model) viewStatusBar() string {
	left := "  esc:unfocus  q:quit"
	right := fmt.Sprintf("%d tables ", len(m.tables))
	gap := m.w - m.sidebarW - len(left) - len(right) - 4
	if gap < 2 {
		gap = 2
	}
	return sStatus.Width(m.w - m.sidebarW).Render(left + strings.Repeat(" ", gap) + right)
}

// ── Dashboard ──

func (m model) viewDash() string {
	var b strings.Builder

	b.WriteString("\n")
	b.WriteString(sTitle.Render("  Database Overview") + "\n")
	b.WriteString(sDim.Render("  Encrypted DiagDocDb.sqlite from BMW ISTA") + "\n\n")

	// Stats cards
	stats := []struct {
		label string
		value string
		color lipgloss.Color
	}{
		{"Tables", fmt.Sprintf("%d", len(m.tables)), cBlue},
		{"Total Rows", fmtNum(m.total), cGreen},
		{"Largest", m.tables[0].Name, cCyan},
		{"Largest Rows", fmtNum(m.tables[0].Rows), cOrange},
		{"Password", m.cfg.Password, cPurple},
		{"DB Size", "7.6 GB", cDim},
	}

	cardW := (m.w - m.sidebarW - 8) / 3
	if cardW < 20 {
		cardW = 20
	}

	for i := 0; i < len(stats); i += 3 {
		var cards []string
		for j := 0; j < 3 && i+j < len(stats); j++ {
			s := stats[i+j]
			card := lipgloss.JoinVertical(lipgloss.Left,
				sStatLabel.Render(s.label),
				lipgloss.NewStyle().Foreground(s.color).Bold(true).Render(s.value),
			)
			cards = append(cards, sBox.Width(cardW).Height(3).Render(card))
		}
		b.WriteString("  " + lipgloss.JoinHorizontal(lipgloss.Top, cards...) + "\n")
	}

	// Top tables
	b.WriteString("\n")
	b.WriteString(sBlue.Render("  ▦ Largest Tables") + "\n\n")

	topN := 15
	if m.h < 30 {
		topN = 8
	}
	if len(m.tables) < topN {
		topN = len(m.tables)
	}

	maxBar := 30
	for i := 0; i < topN; i++ {
		t := m.tables[i]
		barLen := 0
		if m.tables[0].Rows > 0 {
			barLen = (t.Rows * maxBar) / m.tables[0].Rows
		}
		bar := lipgloss.NewStyle().Foreground(cBlue).Render(strings.Repeat("█", barLen))
		name := lipgloss.NewStyle().Width(32).Render(fmt.Sprintf("  %-30s", t.Name))
		cnt := sCyan.Render(fmtNum(t.Rows))
		b.WriteString(name + " " + bar + " " + cnt + "\n")
	}

	return b.String()
}

// ── Tables ──

func (m model) viewTables() string {
	var b strings.Builder
	b.WriteString("\n")

	header := sTitle.Render("  ▦ Table Browser") + "  "
	if m.filterIn.Focused() {
		header += sBlue.Render("filter: ") + m.filterIn.View()
	} else {
		header += sDim.Render("/ to filter • " + fmt.Sprintf("%d results", len(m.tblRows)))
	}
	b.WriteString(header + "\n\n")

	b.WriteString(m.tblTable.View())
	return b.String()
}

// ── SQL ──

func (m model) viewSQL() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(sTitle.Render("  ▸ SQL Query") + "\n")
	b.WriteString(sDim.Render("  SELECT only • ↑↓ history • Enter to execute") + "\n\n")

	if m.sqlIn.Focused() {
		b.WriteString("  " + sBlue.Render("›") + " " + m.sqlIn.View() + "\n")
	} else {
		b.WriteString("  " + sDim.Render("press Enter or i to type query") + "\n")
	}

	if m.sqlErr != "" {
		b.WriteString("\n  " + sRed.Render("✗ "+m.sqlErr) + "\n")
	}

	if len(m.sqlRows) > 0 {
		b.WriteString("\n  " + sGreen.Render(fmt.Sprintf("✓ %d rows", len(m.sqlRows))) +
			sDim.Render(fmt.Sprintf(" in %s", m.sqlTime.Round(time.Millisecond))) + "\n\n")
		b.WriteString(m.renderResultTable(m.sqlCols, m.sqlRows))
	}

	return b.String()
}

// ── VIN ──

func (m model) viewVIN() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(sTitle.Render("  ◈ VIN Lookup") + "\n")
	b.WriteString(sDim.Render("  7.9M VIN ranges • full VIN or positions 4-7") + "\n\n")

	b.WriteString("  VIN: " + m.vinIn.View() + "\n")

	if m.vinErr != "" {
		b.WriteString("\n  " + sRed.Render("✗ "+m.vinErr) + "\n")
	}

	if len(m.vinRows) > 0 {
		b.WriteString("\n  " + sGreen.Render(fmt.Sprintf("✓ %d match(es)", len(m.vinRows))) + "\n\n")

		// Deduplicate by type key + range
		type entry struct {
			TypeKey, VIN47, From, To, Year, Month, Gearbox string
		}
		seen := map[string]bool{}
		var entries []entry
		for _, r := range m.vinRows {
			e := entry{
				TypeKey:  fmt.Sprintf("%v", r["TYPSCHLUESSEL"]),
				VIN47:    fmt.Sprintf("%v", r["VIN17_4_7"]),
				From:     fmt.Sprintf("%v", r["VINBANDFROM"]),
				To:       fmt.Sprintf("%v", r["VINBANDTO"]),
				Year:     fmt.Sprintf("%v", r["PRODUCTIONDATEYEAR"]),
				Month:    fmt.Sprintf("%v", r["PRODUCTIONDATEMONTH"]),
				Gearbox:  fmt.Sprintf("%v", r["GEARBOX_TYPE"]),
			}
			key := e.TypeKey + e.From
			if seen[key] {
				continue
			}
			seen[key] = true
			entries = append(entries, e)
		}

		cols := []string{"TYPE KEY", "VIN4-7", "FROM", "TO", "YEAR", "MONTH", "GBX"}
		var rows []map[string]interface{}
		for _, e := range entries {
			rows = append(rows, map[string]interface{}{
				"TYPE KEY": e.TypeKey, "VIN4-7": e.VIN47,
				"FROM": e.From, "TO": e.To,
				"YEAR": e.Year, "MONTH": e.Month, "GBX": e.Gearbox,
			})
			if len(rows) >= 20 {
				break
			}
		}
		b.WriteString(m.renderResultTable(cols, rows))
	}

	return b.String()
}

// ── Codes ──

func (m model) viewCodes() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(sTitle.Render("  ◇ Code Lookup") + "\n")
	b.WriteString(sDim.Render("  Tab to switch type • Enter to search") + "\n\n")

	types := []string{"P-Code", "Fault", "CC Msg", "Diag Code"}
	var tabs []string
	for i, t := range types {
		if i == m.codeType {
			tabs = append(tabs, sNavActive.Render(t))
		} else {
			tabs = append(tabs, sNavInactive.Render(t))
		}
	}
	b.WriteString("  " + lipgloss.JoinHorizontal(lipgloss.Top, tabs...) + "\n\n")

	b.WriteString("  Search: " + m.codeIn.View() + "\n")

	if m.codeErr != "" {
		b.WriteString("\n  " + sRed.Render("✗ "+m.codeErr) + "\n")
	}

	if len(m.codeRows) > 0 {
		b.WriteString("\n  " + sGreen.Render(fmt.Sprintf("✓ %d result(s)", len(m.codeRows))) + "\n\n")

		var cols []string
		for k := range m.codeRows[0] {
			cols = append(cols, k)
		}
		sort.Strings(cols)
		if len(cols) > 5 {
			cols = cols[:5]
		}

		display := m.codeRows
		if len(display) > 20 {
			display = display[:20]
		}
		b.WriteString(m.renderResultTable(cols, display))
	}

	return b.String()
}

// ── Export ──

func (m model) viewExport() string {
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(sTitle.Render("  ↓ Export Tables") + "\n")
	b.WriteString(sDim.Render("  Export all tables as JSON files") + "\n\n")

	b.WriteString(sBox.Width(50).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			fmt.Sprintf("  Tables:  %d", len(m.tables)),
			fmt.Sprintf("  Output:  db-export/"),
			fmt.Sprintf("  Format:  JSON"),
			"",
			sBlue.Render("  Press Enter to export"),
		),
	))

	if len(m.expLog) > 0 {
		b.WriteString("\n\n  Export log:\n")
		for _, l := range m.expLog {
			b.WriteString(l + "\n")
		}
	}

	return b.String()
}

// ── Helpers ──

func (m model) renderResultTable(cols []string, rows []map[string]interface{}) string {
	if len(rows) == 0 || len(cols) == 0 {
		return sDim.Render("  (no results)")
	}

	columns := []table.Column{}
	for _, c := range cols {
		w := 18
		if len(c) > 14 {
			w = len(c) + 3
		}
		if w > 40 {
			w = 40
		}
		columns = append(columns, table.Column{Title: c, Width: w})
	}

	var tRows []table.Row
	for _, r := range rows {
		var row table.Row
		for _, c := range cols {
			v := r[c]
			if v == nil {
				row = append(row, "—")
			} else {
				s := fmt.Sprintf("%v", v)
				if len(s) > 38 {
					s = s[:35] + "..."
				}
				row = append(row, s)
			}
		}
		tRows = append(tRows, row)
	}

	maxH := m.h - 20
	if maxH < 3 {
		maxH = 3
	}
	if len(tRows) > maxH {
		tRows = tRows[:maxH]
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithRows(tRows),
		table.WithHeight(len(tRows)+1),
	)
	ts := table.DefaultStyles()
	ts.Header = sHeaderCell
	t.SetStyles(ts)

	return "  " + strings.ReplaceAll(t.View(), "\n", "\n  ")
}

func fmtNum(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var r []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			r = append(r, ',')
		}
		r = append(r, byte(c))
	}
	return string(r)
}
