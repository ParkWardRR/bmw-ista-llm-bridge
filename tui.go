package main

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/corona10/goimagehash"
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
	istaFound   bool
	encoder     *Encoder
	encoderErr  error
	sessions    []Session
	sessionsErr error
}

type watchUpdateMsg struct {
	count    int
	size     int64
	pSkips   int64
	lastFile string
}

type watchStartedMsg struct{ title string }
type watchErrorMsg struct{ err string }
type watchStoppedMsg struct{}

type bundleResultMsg struct {
	dir     string
	files   []string
	vehicle string
	err     string
}

type tickMsg time.Time

// ---------------------------------------------------------------------------
// Views
// ---------------------------------------------------------------------------

type tuiView int

const (
	viewDashboard tuiView = iota
	viewSessions
	viewWatch
	viewBundle
)

// ---------------------------------------------------------------------------
// Model
// ---------------------------------------------------------------------------

type tuiModel struct {
	cfg    Config
	logger *slog.Logger
	view   tuiView
	width  int
	height int

	// Initialisation
	loading     bool
	istaFound   bool
	encoder     *Encoder
	encoderDesc string
	encoderErr  error
	sessions    []Session
	sessionsErr error

	sp spinner.Model

	// Sessions table
	tbl table.Model

	// Watch state
	wRunning  bool
	wTitle    string
	wCount    int
	wSize     int64
	wPSkips   int64
	wStart    time.Time
	wLastFile string
	wErr      string
	wStopCh   chan struct{}
	wUpdateCh chan watchUpdateMsg

	// Bundle state
	bRunning bool
	bTarget  *Session
	bDone    bool
	bDir     string
	bFiles   []string
	bVehicle string
	bErr     string
}

func newTUIModel() tuiModel {
	cfg := loadConfig()
	logger := setupLogging(cfg.Logging)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(bmwBlue)

	return tuiModel{
		cfg:     cfg,
		logger:  logger,
		view:    viewDashboard,
		loading: true,
		sp:      sp,
	}
}

// ---------------------------------------------------------------------------
// Init
// ---------------------------------------------------------------------------

func (m tuiModel) Init() tea.Cmd {
	return tea.Batch(m.sp.Tick, doInitCmd(m.cfg, m.logger))
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
		m.istaFound = msg.istaFound
		m.encoder = msg.encoder
		m.encoderErr = msg.encoderErr
		if m.encoder != nil {
			m.encoderDesc = fmt.Sprintf("%s  %s  %s",
				m.encoder.Accel, m.encoder.Codec, strings.ToUpper(m.encoder.Ext))
		}
		m.sessions = msg.sessions
		m.sessionsErr = msg.sessionsErr
		return m, nil

	case tickMsg:
		if m.view == viewWatch && m.wRunning {
			return m, tickCmd()
		}
		return m, nil

	case watchStartedMsg:
		if m.view != viewWatch {
			return m, nil
		}
		m.wTitle = msg.title
		m.wStart = time.Now()
		m.wRunning = true
		return m, tea.Batch(listenWatchCmd(m.wUpdateCh), tickCmd())

	case watchErrorMsg:
		m.wErr = msg.err
		m.wRunning = false
		return m, nil

	case watchUpdateMsg:
		m.wCount = msg.count
		m.wSize = msg.size
		m.wPSkips = msg.pSkips
		if msg.lastFile != "" {
			m.wLastFile = msg.lastFile
		}
		return m, listenWatchCmd(m.wUpdateCh)

	case watchStoppedMsg:
		m.wRunning = false
		return m, nil

	case bundleResultMsg:
		m.bRunning = false
		m.bDone = true
		if msg.err != "" {
			m.bErr = msg.err
		} else {
			m.bDir = msg.dir
			m.bFiles = msg.files
			m.bVehicle = msg.vehicle
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// Key handling
// ---------------------------------------------------------------------------

func (m tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		if m.view == viewWatch && m.wRunning {
			close(m.wStopCh)
			m.wRunning = false
			return m, nil
		}
		return m, tea.Quit
	}

	switch m.view {
	case viewDashboard:
		return m.keyDashboard(msg)
	case viewSessions:
		return m.keySessions(msg)
	case viewWatch:
		return m.keyWatch(msg)
	case viewBundle:
		return m.keyBundle(msg)
	}
	return m, nil
}

func (m tuiModel) keyDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.loading {
		return m, nil
	}
	switch msg.String() {
	case "w":
		if m.encoder == nil {
			m.wErr = "No encoder available. Install ffmpeg and restart."
			m.view = viewWatch
			return m, nil
		}
		m.view = viewWatch
		m.wErr = ""
		m.wCount = 0
		m.wSize = 0
		m.wPSkips = 0
		m.wLastFile = ""
		m.wTitle = ""
		m.wRunning = false
		m.wStopCh = make(chan struct{})
		m.wUpdateCh = make(chan watchUpdateMsg, 64)
		return m, tea.Batch(
			m.sp.Tick,
			startWatchCmd(m.cfg, m.logger, m.encoder, m.wStopCh, m.wUpdateCh),
		)
	case "s":
		m.view = viewSessions
		m.tbl = buildTable(m.sessions)
		return m, nil
	case "b":
		if len(m.sessions) == 0 {
			return m, nil
		}
		return m.beginBundle(&m.sessions[0])
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
	case tea.KeyEnter:
		idx := m.tbl.Cursor()
		if idx >= 0 && idx < len(m.sessions) {
			return m.beginBundle(&m.sessions[idx])
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m tuiModel) keyWatch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEsc {
		if m.wRunning {
			close(m.wStopCh)
			m.wRunning = false
			return m, nil
		}
		m.view = viewDashboard
		return m, nil
	}
	// Any key when capture has stopped returns to dashboard.
	if !m.wRunning && m.wTitle != "" {
		m.view = viewDashboard
		return m, nil
	}
	return m, nil
}

func (m tuiModel) keyBundle(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.bDone || m.bErr != "" {
		m.view = viewDashboard
		m.bDone = false
		m.bErr = ""
		return m, nil
	}
	return m, nil
}

func (m tuiModel) beginBundle(s *Session) (tea.Model, tea.Cmd) {
	m.view = viewBundle
	m.bRunning = true
	m.bDone = false
	m.bErr = ""
	m.bTarget = s
	m.bFiles = nil
	m.bVehicle = ""
	m.bDir = ""
	return m, tea.Batch(
		m.sp.Tick,
		doBundleCmd(m.logger, *s, m.cfg.Output.Directory),
	)
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
	case viewWatch:
		content = m.viewWatch()
	case viewBundle:
		content = m.viewBundle()
	}
	return lipgloss.NewStyle().Padding(1, 2).Render(content)
}

func (m tuiModel) viewDashboard() string {
	var b strings.Builder

	banner := styleBanner.Render(
		styleTitle.Render("BMW ISTA LLM Bridge") + "\n" +
			styleSubtitle.Render("Diagnostic Session Capture & Bundle"))
	b.WriteString(banner + "\n\n")

	if m.loading {
		b.WriteString(m.sp.View() + " Initializing...\n")
		return b.String()
	}

	b.WriteString(styleSection.Render("System Status") + "\n\n")

	// ISTA install
	if m.istaFound {
		b.WriteString(styleLabel.Render("ISTA Install") +
			styleOK.Render("Found") +
			styleSubtitle.Render("  "+m.cfg.ISTA.InstallDir) + "\n")
	} else {
		b.WriteString(styleLabel.Render("ISTA Install") +
			styleFail.Render("Not found") +
			styleSubtitle.Render("  "+m.cfg.ISTA.InstallDir) + "\n")
	}

	// Encoder
	if m.encoderErr != nil {
		b.WriteString(styleLabel.Render("Encoder") +
			styleFail.Render(m.encoderErr.Error()) + "\n")
	} else if m.encoder != nil {
		b.WriteString(styleLabel.Render("Encoder") +
			styleOK.Render(m.encoderDesc) + "\n")
	} else {
		b.WriteString(styleLabel.Render("Encoder") +
			styleSubtitle.Render("--") + "\n")
	}

	// Sessions
	if m.sessionsErr != nil {
		b.WriteString(styleLabel.Render("Sessions") +
			styleFail.Render(m.sessionsErr.Error()) + "\n")
	} else if len(m.sessions) > 0 {
		info := fmt.Sprintf("%d discovered", len(m.sessions))
		latest := m.sessions[0].Timestamp.Format("2006-01-02")
		b.WriteString(styleLabel.Render("Sessions") +
			styleNeutral.Render(info) +
			styleSubtitle.Render("  latest "+latest) + "\n")
	} else {
		b.WriteString(styleLabel.Render("Sessions") +
			styleSubtitle.Render("None found") + "\n")
	}

	b.WriteString("\n" + styleSep.Render(strings.Repeat("-", 52)) + "\n\n")

	type mi struct{ k, d string }
	menu := []mi{{"w", "Watch"}, {"s", "Sessions"}, {"b", "Bundle"}, {"q", "Quit"}}
	parts := make([]string, len(menu))
	for i, it := range menu {
		parts[i] = styleMenuKey.Render("["+it.k+"]") + " " + styleMenuDesc.Render(it.d)
	}
	b.WriteString(strings.Join(parts, "   ") + "\n")

	return b.String()
}

func (m tuiModel) viewSessions() string {
	var b strings.Builder

	header := styleTitle.Render("Sessions")
	if len(m.sessions) > 0 {
		header += styleSubtitle.Render(fmt.Sprintf("  %d found", len(m.sessions)))
	}
	b.WriteString(header + "\n\n")

	if len(m.sessions) == 0 {
		b.WriteString(styleSubtitle.Render("No sessions discovered.") + "\n")
		b.WriteString(styleSubtitle.Render("Looked in: "+
			filepath.Join(m.cfg.ISTA.InstallDir, "Transactions")) + "\n")
	} else {
		b.WriteString(m.tbl.View() + "\n")
	}

	b.WriteString("\n")
	b.WriteString(styleHelp.Render("Up/Down: Navigate   Enter: Bundle selected   Esc: Back"))
	return b.String()
}

func (m tuiModel) viewWatch() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Watch") +
		styleSubtitle.Render("  Live Capture") + "\n\n")

	if m.wErr != "" {
		b.WriteString(styleError.Render("Error: "+m.wErr) + "\n\n")
		b.WriteString(styleHelp.Render("Press any key to return"))
		return b.String()
	}

	if !m.wRunning && m.wCount == 0 && m.wTitle == "" {
		b.WriteString(m.sp.View() + " Searching for ISTA window...\n")
		return b.String()
	}

	dur := time.Since(m.wStart)
	var lines []string
	lines = append(lines, styleLabel.Render("Window")+styleNeutral.Render(m.wTitle))
	lines = append(lines, styleLabel.Render("Duration")+styleNeutral.Render(fmtDuration(dur)))
	lines = append(lines, styleLabel.Render("Screenshots")+styleNeutral.Render(fmt.Sprintf("%d", m.wCount)))
	lines = append(lines, styleLabel.Render("Total Size")+styleNeutral.Render(fmtBytes(m.wSize)))
	lines = append(lines, styleLabel.Render("pHash Skips")+styleNeutral.Render(fmt.Sprintf("%d", m.wPSkips)))
	if m.wLastFile != "" {
		lines = append(lines, styleLabel.Render("Last File")+styleNeutral.Render(m.wLastFile))
	}

	b.WriteString(styleBox.Render(strings.Join(lines, "\n")) + "\n\n")

	if m.wRunning {
		b.WriteString(m.sp.View() + " Monitoring for changes...\n\n")
		b.WriteString(styleHelp.Render("Esc or Ctrl+C to stop capture"))
	} else {
		b.WriteString(styleOK.Render("Capture stopped.") + "\n\n")
		b.WriteString(styleHelp.Render("Press any key to return"))
	}

	return b.String()
}

func (m tuiModel) viewBundle() string {
	var b strings.Builder

	b.WriteString(styleTitle.Render("Bundle") + "\n\n")

	if m.bTarget != nil {
		b.WriteString(styleLabel.Render("Session") +
			styleNeutral.Render(
				m.bTarget.Timestamp.Format("2006-01-02 15:04")+
					"  "+m.bTarget.VIN) + "\n")
		if m.bTarget.Model != "" {
			b.WriteString(styleLabel.Render("Model") +
				styleNeutral.Render(m.bTarget.Model) + "\n")
		}
	}

	if m.bVehicle != "" {
		b.WriteString(styleLabel.Render("Vehicle") +
			styleNeutral.Render(m.bVehicle) + "\n")
	}

	b.WriteString("\n")

	if m.bRunning {
		b.WriteString(m.sp.View() + " Bundling session...\n")
		return b.String()
	}

	if m.bErr != "" {
		b.WriteString(styleError.Render("Error: "+m.bErr) + "\n\n")
		b.WriteString(styleHelp.Render("Press any key to return"))
		return b.String()
	}

	if m.bDone {
		if len(m.bFiles) > 0 {
			var fl []string
			for _, f := range m.bFiles {
				fl = append(fl, styleOK.Render("  + ")+styleNeutral.Render(f))
			}
			b.WriteString(styleBox.Render(strings.Join(fl, "\n")) + "\n\n")
		}
		b.WriteString(styleLabel.Render("Output") +
			styleSubtitle.Render(m.bDir) + "\n\n")
		b.WriteString(styleOK.Render("Bundle complete!") + "\n\n")
		b.WriteString(styleHelp.Render("Press any key to return"))
	}

	return b.String()
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

func doInitCmd(cfg Config, logger *slog.Logger) tea.Cmd {
	return func() tea.Msg {
		var r initResultMsg
		info, err := os.Stat(cfg.ISTA.InstallDir)
		r.istaFound = err == nil && info.IsDir()
		r.encoder, r.encoderErr = probeEncoder(logger, cfg.Encoding.Quality, cfg.Encoding.Preset)
		r.sessions, r.sessionsErr = discoverSessions(cfg)
		return r
	}
}

func startWatchCmd(
	cfg Config,
	logger *slog.Logger,
	enc *Encoder,
	stopCh <-chan struct{},
	updateCh chan<- watchUpdateMsg,
) tea.Cmd {
	return func() tea.Msg {
		setDPIAware()

		hwnd, winTitle, err := findWindowByTitle(logger, cfg.Window.Title)
		if err != nil {
			return watchErrorMsg{err: fmt.Sprintf("Window not found: %v", err)}
		}

		sessionDir := cfg.Output.Directory
		if cfg.Output.SessionFolders {
			sessionDir = filepath.Join(cfg.Output.Directory, time.Now().Format("2006-01-02"))
		}
		if err := os.MkdirAll(sessionDir, 0755); err != nil {
			return watchErrorMsg{err: fmt.Sprintf("Cannot create output dir: %v", err)}
		}

		go runCaptureLoop(cfg, logger, enc, hwnd, sessionDir, stopCh, updateCh)
		return watchStartedMsg{title: winTitle}
	}
}

func runCaptureLoop(
	cfg Config,
	logger *slog.Logger,
	enc *Encoder,
	hwnd syscall.Handle,
	sessionDir string,
	stopCh <-chan struct{},
	updateCh chan<- watchUpdateMsg,
) {
	defer close(updateCh)

	var prev []byte
	var prevW, prevH int
	var prevHash *goimagehash.ImageHash
	var count int
	var totalBytes int64
	var pHashSkips int64

	pollDur := time.Duration(cfg.Capture.PollMs) * time.Millisecond
	debounceDur := time.Duration(cfg.Capture.DebounceMs) * time.Millisecond
	ticker := time.NewTicker(pollDur)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
		}

		if !isWindowValid(hwnd) {
			return
		}

		pixels, w, h, err := captureWindow(hwnd)
		if err != nil {
			continue
		}

		if prev != nil && w == prevW && h == prevH {
			// Fast perceptual-hash pre-check.
			if cfg.Capture.PHashThreshold > 0 && prevHash != nil {
				hash, herr := computePHash(pixels, w, h)
				if herr == nil {
					dist, _ := hash.Distance(prevHash)
					if dist <= cfg.Capture.PHashThreshold {
						pHashSkips++
						continue
					}
				}
			}

			// Pixel-level diff check.
			ratio := diffRatio(prev, pixels)
			if ratio < cfg.Capture.Threshold {
				continue
			}

			logger.Debug("change detected", "diff", fmt.Sprintf("%.2f%%", ratio*100))

			// Debounce: wait for the screen to settle, but respect stop.
			select {
			case <-stopCh:
				return
			case <-time.After(debounceDur):
			}

			pixels, w, h, err = captureWindow(hwnd)
			if err != nil {
				continue
			}
		}

		prev = pixels
		prevW = w
		prevH = h
		prevHash, _ = computePHash(pixels, w, h)

		ts := time.Now().Format("15-04-05.000")
		outPath := filepath.Join(sessionDir, fmt.Sprintf("ista_%s.%s", ts, enc.Ext))

		if err := encode(enc, pixels, w, h, outPath); err != nil {
			logger.Error("encode failed", "error", err)
			continue
		}

		fi, _ := os.Stat(outPath)
		if fi != nil {
			count++
			totalBytes += fi.Size()

			select {
			case updateCh <- watchUpdateMsg{
				count:    count,
				size:     totalBytes,
				pSkips:   pHashSkips,
				lastFile: filepath.Base(outPath),
			}:
			default:
			}
		}
	}
}

func listenWatchCmd(ch <-chan watchUpdateMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return watchStoppedMsg{}
		}
		return msg
	}
}

func doBundleCmd(logger *slog.Logger, s Session, outDir string) tea.Cmd {
	return func() tea.Msg {
		if err := s.ParseMeta(); err != nil {
			logger.Warn("meta parse skipped", "error", err)
		}
		if err := s.ParseTrans(); err != nil {
			logger.Warn("trans parse skipped", "error", err)
		}

		if err := bundleSession(logger, &s, outDir); err != nil {
			return bundleResultMsg{err: err.Error()}
		}

		bundleDir := filepath.Join(outDir,
			fmt.Sprintf("session_%s_%s", s.Timestamp.Format("2006-01-02_150405"), s.VIN))

		var files []string
		entries, _ := os.ReadDir(bundleDir)
		for _, e := range entries {
			files = append(files, e.Name())
		}

		vehicle := ""
		if s.Meta != nil {
			var parts []string
			if s.Meta.Features.Brand != "" {
				parts = append(parts, s.Meta.Features.Brand)
			}
			if s.Meta.Features.ModelSeries != "" {
				parts = append(parts, s.Meta.Features.ModelSeries)
			}
			if s.Meta.Features.Engine != "" {
				parts = append(parts, s.Meta.Features.Engine)
			}
			vehicle = strings.Join(parts, " ")
		}

		return bundleResultMsg{
			dir:     bundleDir,
			files:   files,
			vehicle: vehicle,
		}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

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

func fmtDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	mn := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, mn, s)
}

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
