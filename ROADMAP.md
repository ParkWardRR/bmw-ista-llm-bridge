# ista-bridge Roadmap

Capture BMW ISTA diagnostic sessions and package them for Claude/ChatGPT to consume for vehicle troubleshooting.

## Phase 1: Screenshot Capture [DONE]

Silently capture the ISTA GUI as the user works through a diagnostic session.

- [x] Win32 PrintWindow/BitBlt capture with DPI awareness
- [x] pHash pre-filter → pixel diff two-tier change detection
- [x] Debounce (400ms settle time before capture)
- [x] AVIF encoding via FFmpeg (av1_qsv > libaom > svtav1 > webp fallback)
- [x] libaom-av1 with yuv444p10le for perfect text/chroma fidelity
- [x] Date-based session folders
- [x] TOML config at `%APPDATA%\ista-bridge\config.toml`
- [x] Structured logging with rotation (slog + lumberjack)

## Phase 2: ISTA Session Parsing [DONE]

Parse ISTA session artifacts into structured data. ISTA writes rich XML files after every diagnostic session — these contain everything the GUI shows and more.

### Session Discovery
- [x] Auto-discover sessions from `Transactions/RG_META_*.xml`
- [x] Correlate transaction files (META, TRANS, PRG) by VIN+timestamp
- [x] Correlate FASTAOut files (.behdat, .fstdat)
- [x] Correlate zip.log session bundles
- [x] `ista-bridge sessions` — list all discovered sessions

### Vehicle Identification (from RG_META + RG_TRANS)
- [x] VIN, brand, model series (F32), body (COU), engine (N20)
- [x] Transmission, model year, market, steering, assembly country
- [x] I-Level (integration level = software version): `F020-15-11-502`
- [x] Mileage at session time
- [x] Communication type (ENET/OBD/ICOM)

### ECU Enumeration (from RG_TRANS)
- [x] Full ECU list with tree name, full name, variant, SGBD
- [x] Bus assignments (KCAN, BCAN, FACAN, FLEXRAY, ROOT)
- [x] Diagnostic protocol (UDS)
- [x] Supplier name, serial number, manufacture date
- [x] Software version table (SGBM IDs)
- [x] Communication success status

### Fault Codes (from RG_TRANS)
- [x] All DTCs with fault location code and description
- [x] Fault status (present/stored/pending)
- [x] Warning lamp impact
- [x] P-code / SAE code (when available)
- [x] Fault environment data (mileage, temperature)
- [x] Correlated to ECU name and bus

### Data Sources Discovered

| Source | Location | Format | Contents |
|--------|----------|--------|----------|
| RG_META | `Transactions/RG_META_<VIN><TS>.xml` | XML | Vehicle basics, session timing, dealer info |
| RG_TRANS | `Transactions/RG_TRANS_<VIN><TS>.xml` | XML | ECU list, fault codes, I-level, SVK, FFM (richest source) |
| RG_PRG | `Transactions/RG_PRG_<VIN><TS>.xml` | XML | Programming session data (if any) |
| .behdat | `FASTAOut/<slot>_<VIN>_<dealer>_<ts>.behdat` | XML | Session behavioral data, backend requests |
| .fstdat | `FASTAOut/<slot>_<VIN>_<dealer>_<ts>.fstdat` | XML | Vehicle test results (pass/fail) |
| zip.log | `Logs/<date>_<model>_<VIN>.zip.log` | ZIP | Complete log bundle (see below) |

### zip.log Bundle Contents (10 files)

| File | Size | Contents |
|------|------|----------|
| `IstaOperation.log` | ~5 MB | Main operation log — full session flow |
| `psdz_Default.log` | ~7 MB | PSdZ programming default log |
| `psdz.log` | ~2.5 MB | PSdZ programming log |
| `<VIN>_F32.xml` | ~2.5 MB | ECUKom — every EDIABAS job with raw responses |
| `ISTA.log` | ~195 KB | Application startup/config log |
| `RG_TRANS_*.xml` | ~103 KB | Transaction data (copy) |
| `behdat` | ~383 KB | Behavioral data (copy) |
| `PsdzServiceHost.log` | ~57 KB | PSdZ service host log |
| `IstaServicesHost.log` | ~26 KB | WCF services host log |
| `[Content_Types].xml` | ~0.3 KB | Manifest |

## Phase 3: Session Bundling [DONE]

Package a diagnostic session into files an LLM can consume.

- [x] `ista-bridge bundle` — package the latest session
- [x] `ista-bridge bundle -session 2026-09-20` — package a specific date
- [x] `summary.md` — LLM-optimized vehicle + fault + ECU overview
- [x] `vehicle.json` — structured vehicle identity data
- [x] `ecus.json` — full ECU list with software versions
- [x] `faults.json` — all DTCs with status, description, environment data
- [x] Auto-copy screenshots from matching session date folder

### TODO
- [ ] Extract and include ECUKom data from zip.log (EDIABAS job results)
- [ ] Include FASTA test results (pass/fail per test)
- [ ] `timeline.json` — what happened when (session flow from IstaOperation.log)
- [ ] Estimate token count for the bundle
- [ ] Auto-truncate if bundle exceeds LLM context limits

## Phase 3.5: DiagDocDb Access [DONE]

Decrypt and query the encrypted DiagDocDb.sqlite (7.6 GB, 232 tables, 7.9M VIN ranges).

### DB Password Discovery
- [x] .NET PE/CLI public key token extraction from DLLs (`odincs` command)
- [x] Password = uppercase Rheingold assembly strong name public key token
- [x] System.Data.SQLite encryption via `HAS_CODEC` compiled interop (32-bit required)
- [x] Query via 32-bit PowerShell bridge (`db.go`)

### Features
- [x] `db tables` — list all 232 tables
- [x] `db export <table>` — export table as JSON
- [x] `db export-all <dir>` — export all tables
- [x] `db query <sql>` — raw SQL query
- [x] `db schema <table>` — show table schema
- [x] `vin <vin>` — VIN lookup via VINRANGES (7.9M records)
- [x] `lookup pcode <code>` — P-code lookup via CODEASSIGNMENT
- [x] `lookup fault <code>` — BMW fault code lookup via XEP_FAULTCODES
- [x] `lookup cc <text>` — Check Control message search via XEP_CCMESSAGE
- [x] `lookup diag <text>` — Diagnostic code search via XEP_DIAGCODE
- [x] `report` — Generate markdown diagnostic report from session data
- [x] `odincs <dll>` — Extract .NET public key tokens from DLLs

## Phase 3.6: Interactive TUI [DONE]

Full-featured terminal UI (Bubble Tea) for interactive diagnostic workflows without memorizing CLI flags.

### Dashboard
- [x] System status overview (ISTA install, encoder, sessions, DiagDocDb connectivity)
- [x] Capture statistics (screenshots, total size, session count)
- [x] Menu-driven navigation with keyboard shortcuts

### Database Views (shown when DiagDocDb is accessible)
- [x] VIN lookup — text input with async database query and formatted results
- [x] Fault code lookup — multi-mode search (P-code, fault code, Check Control, diag code) with Tab/number-key mode switching
- [x] Report generation — auto-generates Markdown diagnostic report from latest session with progress spinner

### UX
- [x] Bubble Tea state machine with view routing (dashboard → VIN/lookup/report → results)
- [x] Async database queries with loading spinners
- [x] Rounded-box result cards with labeled fields
- [x] Text input with cursor blink, placeholder text, and Esc-to-back navigation
- [x] Graceful degradation — DB features hidden when DiagDocDb is not accessible

## Phase 4: LLM Integration

Make it trivial to go from ISTA session to AI-assisted diagnosis.

- [ ] `ista-bridge ask` — bundle + send to Claude API in one command
- [ ] Clipboard mode: `ista-bridge bundle --clipboard` copies summary.md to clipboard
- [ ] Prompt template with vehicle context preamble
- [ ] Multi-turn: follow-up questions referencing specific screenshots or DTCs
- [ ] Cost estimate before sending (token count × model pricing)

## Phase 5: Distribution

- [ ] GoReleaser for single-binary builds
- [ ] GitHub releases with checksums
- [ ] Winget / Scoop package

---

## Removed (out of scope)

These were built but removed — they're desktop app polish that doesn't serve the core goal of getting ISTA data into an LLM:

- ~~System tray~~ (gogpu/systray)
- ~~Global hotkeys~~ (golang.design/x/hotkey)
- ~~Toast notifications~~ (go-toast/toast)
- ~~Auto-start with Windows~~ (registry HKCU Run)

If tray/hotkey support is wanted later, the code is in git stash `pre-cleanup`.

---

## ISTA Data Sources (Reference)

What's extractable and how:

| Source | Method | Data |
|--------|--------|------|
| ISTA GUI | Screenshots (PrintWindow) | Everything visible: fault codes, ECU details, wiring diagrams, test plans |
| RG_TRANS XML | XML parsing (stdlib) | ECU list, fault codes, I-level, SVK, FFM — richest single source |
| RG_META XML | XML parsing (stdlib) | Vehicle basics (model, engine, year), session timing, dealer |
| FASTAOut XML | XML parsing (stdlib) | Test results, session behavioral data |
| zip.log bundle | ZIP extraction | Complete session logs, ECUKom (raw EDIABAS jobs), PSdZ data |
| ISTA main log | Text parsing | Session flow, navigation, errors |
| PsdzServiceHost.log | Text parsing | ENET connection details, SWT tokens |
| xmlvalueprimitive.sqlite | SQLite + FTS5 | Diagnostic procedures, technical docs (compressed XML) |
| DiagDocDb.sqlite | SQLite via 32-bit PS bridge | Main decision-tree DB — 232 tables, 7.9M VIN ranges (key = Rheingold assembly public key token) |
| Port 64923 | WCF binary (blocked) | IPC between GUI and services — not HTTP, can't intercept |
| 169.254.37.25:6801 | DoIP/TCP (blocked) | Raw vehicle comms — would need DoIP protocol implementation |

## Polyglot Satellite Tools

Standalone CLI tools in four languages, orchestrated by the Go app. Each reads JSON data exported from DiagDocDb.

| Tool | Language | Role |
|------|----------|------|
| `ista-keyextract` | Odin | Parse .NET PE/CLI metadata to extract assembly public key token (DB password) |
| `ista-faultlookup` | Gleam/Erlang | Fault code search — BMW codes, P-codes, descriptions |
| `ista-vinlookup` | Zig | Fast VIN decoder — binary search over 7.9M VINRANGES records |
| `ista-report` | Nim | Diagnostic report generator — Markdown/text from exported data |

Password handling: tools accept `--keyfile path/to/ista_keys.txt` or `--dll path/to/ISTAGUI.exe` (Odin extracts the key from the DLL). Key file is gitignored and lives on the user's desktop.

## Technology

| Component | Choice | Why |
|-----------|--------|-----|
| Language | Go | Single binary, no runtime, great Win32 syscall support |
| Capture | PrintWindow/BitBlt | Works for WPF apps, no driver needed |
| Encoding | libaom-av1 (yuv444p10le) | Perfect text fidelity at ~30-50 KB per screenshot |
| Change detection | pHash + pixel diff | Fast rejection of unchanged frames |
| Session parsing | encoding/xml (stdlib) | No external deps for XML parsing |
| DB access | 32-bit PowerShell bridge | Required because SQLite.Interop.dll (SEE) is x86 |
| Config | TOML | Human-readable, comments, designed for config files |
| Logging | slog + lumberjack | stdlib + rotation |
