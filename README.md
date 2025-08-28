<p align="center">
  <h1 align="center">ista-bridge</h1>
  <p align="center">
    Capture BMW ISTA diagnostic sessions and package them for LLM-assisted vehicle troubleshooting.
  </p>
</p>

<p align="center">
  <a href="https://go.dev"><img src="https://img.shields.io/badge/Go-1.27+-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-BlueOak--1.0.0-2D7FEE?style=flat-square" alt="License"></a>
  <a href="https://microsoft.com/windows"><img src="https://img.shields.io/badge/Platform-Windows-0078D4?style=flat-square&logo=windows11&logoColor=white" alt="Platform"></a>
  <a href="https://ffmpeg.org"><img src="https://img.shields.io/badge/FFmpeg-Required-007808?style=flat-square&logo=ffmpeg&logoColor=white" alt="FFmpeg"></a>
  <a href="https://aomediacodec.github.io/av1-avif/"><img src="https://img.shields.io/badge/Format-AVIF-FF6600?style=flat-square" alt="AVIF"></a>
</p>

<p align="center">
  <strong>Hardware Acceleration</strong><br>
  <img src="https://img.shields.io/badge/Intel-QSV-0071C5?style=flat-square&logo=intel&logoColor=white" alt="Intel QSV">
  <img src="https://img.shields.io/badge/NVIDIA-NVENC-76B900?style=flat-square&logo=nvidia&logoColor=white" alt="NVIDIA NVENC">
  <img src="https://img.shields.io/badge/AMD-AMF-ED1C24?style=flat-square&logo=amd&logoColor=white" alt="AMD AMF">
  <img src="https://img.shields.io/badge/CPU-AVX2%20%7C%20NEON-333333?style=flat-square" alt="CPU SIMD">
</p>

---

## What is this?

**ista-bridge** captures everything that happens during a BMW ISTA diagnostic session — screenshots, fault codes, ECU data, vehicle identity — and packages it into files that AI models (Claude, ChatGPT, etc.) can consume to help you troubleshoot your car.

### The problem

BMW ISTA is dealer-level diagnostic software. It reads fault codes, shows ECU trees, runs guided troubleshooting, and displays wiring diagrams — but everything is locked inside a WPF desktop GUI. There's no export button, no API, no JSON. If you want an LLM to help you diagnose your car, you'd have to manually screenshot everything and type out the fault codes.

### The solution

Run ISTA, diagnose your car, and **ista-bridge** silently does three things:

1. **Captures screenshots** every time the ISTA screen changes (AVIF, ~30-50 KB each)
2. **Parses ISTA's XML session files** to extract structured vehicle data
3. **Bundles everything** into a clean package you can feed to an LLM

Then you paste `summary.md` into Claude or ChatGPT and ask *"what's wrong with my car?"* — with full context.

---

## Quick Start

### Prerequisites

| Requirement | Why |
|---|---|
| **Windows 10/11** | ISTA is Windows-only; ista-bridge uses Win32 APIs |
| **Go 1.27+** | Build the binary |
| **FFmpeg** | AVIF screenshot encoding ([download](https://ffmpeg.org/download.html), add to PATH) |
| **BMW ISTA** | The diagnostic software itself (installed at `C:\EC-APPS\ISTA\`) |

### Install

```bash
git clone https://github.com/ParkWardRR/bmw-ista-llm-bridge.git
cd bmw-ista-llm-bridge
go build -o ista-bridge.exe .
```

That's it — single binary, no runtime dependencies.

### Basic Usage

```bash
# 1. Start ISTA and connect to your car

# 2. In another terminal, start capturing
ista-bridge.exe watch

# 3. Do your diagnostic work in ISTA (read faults, run tests, etc.)
# ista-bridge silently captures every screen change

# 4. Press Ctrl+C when done

# 5. Bundle the session for an LLM
ista-bridge.exe bundle

# 6. Open summary.md and paste it into Claude/ChatGPT
```

---

## Commands

### `ista-bridge watch` (default)

Captures ISTA screenshots in real-time. This is the default when no subcommand is given.

```bash
ista-bridge.exe [watch] [flags]
```

| Flag | Default | What it does |
|---|---|---|
| `-window` | `ISTA` | Window title to match (substring, scored matching) |
| `-poll` | `200ms` | How often to check for screen changes |
| `-debounce` | `400ms` | Wait time after a change before capturing (lets animations settle) |
| `-threshold` | `0.005` | Minimum pixel change to trigger a capture (0.5%) |
| `-quality` | `14` | Encoder CRF — lower number = higher quality |
| `-preset` | `4` | Encoder speed — lower = slower but better compression |
| `-out` | `screenshots` | Where to save captures |

**How change detection works:**

1. **pHash pre-filter** (~4ms) — perceptual hash rejects obviously-unchanged frames
2. **Pixel diff** — 64-bit block comparison counts changed pixels (see [Hardware Acceleration](#hardware-acceleration))
3. **Debounce** — waits for the screen to stop changing before capturing

### `ista-bridge sessions`

Lists all ISTA diagnostic sessions found on this machine.

```
$ ista-bridge.exe sessions
Found 2 session(s):

  1. 2025-03-15 09:22  VIN: WBAXXXXXXXX00000  Model: G20
     ✓ Transaction data
     ✓ Log bundle (.zip.log)
     ✓ FASTA behavioral data
     ✓ FASTA test data

  2. 2025-03-10 14:05  VIN: WBAXXXXXXXX00000  Model: F30
     ✓ Transaction data
     ✓ FASTA test data
```

Sessions are discovered by scanning ISTA's `Transactions/` directory for XML files that ISTA writes automatically after every diagnostic session.

### `ista-bridge bundle`

Packages a session into LLM-ready files.

```bash
ista-bridge.exe bundle [-out DIR] [-session YYYY-MM-DD]
```

| Flag | Default | What it does |
|---|---|---|
| `-out` | `screenshots` | Where to create the bundle folder |
| `-session` | *(latest)* | Specific session date to bundle |

**Output structure:**

```
session_2025-03-15_092200_WBAXXXXXXXX00000/
├── summary.md        ← Feed this to Claude/ChatGPT
├── vehicle.json      ← VIN, model, engine, I-level, mileage
├── ecus.json         ← Full ECU list with software versions
├── faults.json       ← All fault codes with status and context
└── screenshots/      ← AVIF captures from the session
```

**What's in each file:**

| File | Contents | Typical Size |
|---|---|---|
| `summary.md` | LLM-optimized Markdown overview — vehicle table, fault code table, ECU list | ~3-5 KB |
| `vehicle.json` | VIN, brand, model series, engine, transmission, I-level, mileage, market | ~0.5 KB |
| `ecus.json` | Every ECU: name, variant, bus, protocol, supplier, software IDs, fault count | ~5-15 KB |
| `faults.json` | Every DTC: code, description, status (active/stored), warning lamp, mileage | ~1-5 KB |
| `screenshots/` | Chronological AVIF captures of the ISTA GUI | ~30-50 KB each |

---

## Hardware Acceleration

ista-bridge uses hardware acceleration at two levels:

### Screenshot Encoding (GPU)

FFmpeg probes for hardware encoders in priority order and falls back gracefully:

| Priority | Encoder | Hardware | Notes |
|---|---|---|---|
| 1 | `av1_qsv` | Intel Quick Sync | Requires 11th-gen+ Intel CPU |
| 2 | `av1_nvenc` | NVIDIA NVENC | Requires RTX 40-series+ |
| 3 | `av1_amf` | AMD AMF | Requires RX 7000-series+ |
| 4 | `libaom-av1` | CPU | Best quality, slowest |
| 5 | `libsvtav1` | CPU | Good balance of speed/quality |
| 6 | `librav1e` | CPU | Rust-based AV1 encoder |
| 7 | `libwebp` | CPU | WebP fallback if no AV1 support |

The probe runs once at startup — a 64x64 test frame is encoded with each codec until one succeeds.

### Pixel Comparison (CPU SIMD)

The change-detection loop compares two full-resolution pixel buffers every poll cycle. The hot path uses **64-bit block comparison**:

- Processes **2 BGRA pixels per iteration** via `uint64` XOR
- Masks out alpha channels (`0x00FFFFFF00FFFFFF`)
- On **x86-64**, the Go compiler emits native 64-bit XOR ops; with AVX2 available, the tight loop can be auto-vectorized
- On **ARM64**, NEON instructions handle the 64-bit operations natively

This gives ~2-4x throughput vs. byte-by-byte comparison on a typical 1920x1080 ISTA window.

---

## How ISTA Session Parsing Works

> **You don't need to understand any of this to use ista-bridge.** This section is for anyone curious about what ISTA writes to disk and how we extract it.

ISTA writes structured XML files after every diagnostic session. These files contain **more data than the GUI shows** — full ECU inventories, software version tables, supplier information, and fault environment snapshots.

### Data Sources

| Source | Location | What's in it |
|---|---|---|
| **RG_META** | `Transactions/RG_META_<VIN><TS>.xml` | Vehicle identity (model, engine, year, market), session timing, dealer info |
| **RG_TRANS** | `Transactions/RG_TRANS_<VIN><TS>.xml` | ECU list, fault codes, I-level, software versions — **richest single source** |
| **RG_PRG** | `Transactions/RG_PRG_<VIN><TS>.xml` | Programming session data (if coding/flashing was done) |
| **.behdat** | `FASTAOut/<slot>_<VIN>_<dealer>_<ts>.behdat` | Session behavioral data |
| **.fstdat** | `FASTAOut/<slot>_<VIN>_<dealer>_<ts>.fstdat` | Vehicle test results (pass/fail) |
| **zip.log** | `Logs/<date>_<model>_<VIN>.zip.log` | Complete session log bundle (actually a ZIP) |

### What's in the zip.log bundle

The `.zip.log` file is a ZIP archive (despite the extension) containing the full session log set:

| File | Typical Size | Contents |
|---|---|---|
| `IstaOperation.log` | ~5 MB | Complete session flow — every action, screen, and decision |
| `psdz_Default.log` | ~7 MB | PSdZ programming service default log |
| `psdz.log` | ~2.5 MB | PSdZ programming log |
| `<VIN>_<Model>.xml` | ~2.5 MB | ECUKom — every EDIABAS diagnostic job with raw responses |
| `ISTA.log` | ~195 KB | Application startup and configuration |
| `PsdzServiceHost.log` | ~57 KB | Java bridge (jni4net) service host |
| `IstaServicesHost.log` | ~26 KB | WCF IPC services |

### ISTA Architecture (Reverse Engineered)

```
┌─────────────────┐     WCF binary      ┌──────────────────────┐
│   ISTAGUI.exe   │◄───────────────────►│  IstaServicesHost.exe │
│  (.NET 4 / WPF) │    port 64923       │     (.NET / WCF)      │
└────────┬────────┘                      └──────────┬───────────┘
         │                                          │
         │ Win32 UI                                 │ jni4net bridge
         │                                          │
         │                               ┌──────────▼───────────┐
         │                               │  PsdzServiceHost.exe  │
         │                               │      (Java / JRE)     │
         │                               └──────────┬───────────┘
         │                                          │
         │                                          │ Ediabas API
         │                                          │
         └──────────────────────┬───────────────────┘
                                │
                      DoIP / ISO 13400
                      TCP → 169.254.x.x:6801
                                │
                         ┌──────▼──────┐
                         │   Vehicle   │
                         │  (via ENET) │
                         └─────────────┘
```

**Key findings:**
- No REST API — the GUI and services communicate over WCF binary protocol (named pipes / TCP)
- Vehicle communication uses DoIP (Diagnostics over IP) per ISO 13400
- Each diagnostic operation opens a fresh TCP connection to the vehicle
- The `DiagDocDb.sqlite` (7 GB) is encrypted with an unknown key
- The `xmlvalueprimitive_*.sqlite` databases are readable and contain diagnostic procedures as compressed XML with FTS5 search

---

## Configuration

Config lives at `%APPDATA%\ista-bridge\config.toml` — created on first run with sensible defaults.

```toml
[window]
title = "ISTA"                    # Window title to match

[capture]
poll_ms = 200                     # Check for changes every 200ms
debounce_ms = 400                 # Wait 400ms after change before capture
threshold = 0.005                 # 0.5% pixel change triggers capture
phash_threshold = 3               # Perceptual hash sensitivity (0-64)

[encoding]
quality = 14                      # CRF for AVIF (12-16 best for text)
preset = 4                        # Speed (0=slow/best, 6=fast)

[output]
directory = "screenshots"         # Base output directory
session_folders = true            # Create YYYY-MM-DD subdirectories

[logging]
level = "info"                    # debug | info | warn | error
max_size_mb = 10                  # Rotate at 10 MB
max_backups = 5                   # Keep 5 rotated files
max_age_days = 28                 # Delete after 28 days
compress = true                   # gzip rotated logs

[ista]
install_dir = 'C:\EC-APPS\ISTA'   # ISTA installation path
log_dir = 'C:\EC-APPS\ISTA\Logs'  # ISTA log directory
```

---

## Building from Source

```bash
# Clone
git clone https://github.com/ParkWardRR/bmw-ista-llm-bridge.git
cd bmw-ista-llm-bridge

# Build
go build -o ista-bridge.exe .

# Run
.\ista-bridge.exe sessions
```

### Dependencies

| Module | Purpose |
|---|---|
| `corona10/goimagehash` | Perceptual hashing for change detection |
| `pelletier/go-toml/v2` | TOML config file parsing |
| `natefinch/lumberjack.v2` | Log file rotation |
| `charmbracelet/bubbletea` | Interactive terminal UI framework |
| `charmbracelet/lipgloss` | TUI styling and layout |
| `charmbracelet/bubbles` | TUI components (table, spinner) |
| *(stdlib)* `encoding/xml` | ISTA XML session parsing |
| *(stdlib)* `syscall` | Win32 API (PrintWindow, BitBlt, DPI) |

No CGo. No C compiler needed. Pure Go + FFmpeg.

---

## Project Structure

```
bmw-ista-llm-bridge/
├── main.go          # Entry point, subcommand routing, capture loop
├── tui.go           # Bubble Tea interactive terminal UI
├── session.go       # ISTA session discovery and XML parsing
├── bundle.go        # LLM-friendly session bundling (JSON + Markdown)
├── win32.go         # Win32 API: window enumeration, PrintWindow, BitBlt, DPI
├── encode.go        # FFmpeg encoder probing (QSV → NVENC → AMF → CPU)
├── hasher.go        # Perceptual hash (pHash) for fast change rejection
├── config.go        # TOML configuration with defaults
├── logging.go       # Structured logging (slog) with rotation (lumberjack)
├── go.mod
└── go.sum
```

---

## Roadmap

See [ROADMAP.md](ROADMAP.md) for the full phased plan.

| Phase | Status | Description |
|---|---|---|
| 1. Screenshot Capture | **Done** | Win32 capture, pHash+pixel diff, AVIF encoding |
| 2. Session Parsing | **Done** | XML parser for ISTA transaction/meta/FASTA files |
| 3. Session Bundling | **Done** | JSON + Markdown output for LLM consumption |
| 4. LLM Integration | Planned | `ista-bridge ask` — direct Claude/ChatGPT API integration |
| 5. Distribution | Planned | GoReleaser, GitHub releases, Winget/Scoop |

---

## FAQ

<details>
<summary><strong>Does this modify ISTA or my car in any way?</strong></summary>

No. ista-bridge is read-only. It captures screenshots of the ISTA window and reads XML files that ISTA writes to disk. It never communicates with your vehicle or modifies any ISTA files.
</details>

<details>
<summary><strong>What BMW models are supported?</strong></summary>

Any model that ISTA supports — the parser reads ISTA's standard XML output format, which is the same across all platforms (E-series, F-series, G-series, U-series). The XML schema hasn't changed across ISTA versions.
</details>

<details>
<summary><strong>Do I need a GPU for hardware acceleration?</strong></summary>

No. GPU encoding is optional and detected automatically. If no hardware encoder is available, ista-bridge falls back to CPU-based `libaom-av1` which produces excellent quality, just slower. The pixel diff comparison uses 64-bit CPU instructions that are available on any modern x86-64 or ARM64 processor.
</details>

<details>
<summary><strong>How big are the screenshot files?</strong></summary>

AVIF screenshots of a typical 1920x1080 ISTA window are 30-50 KB each with the default quality setting (CRF 14). A full diagnostic session might produce 20-50 screenshots, totaling 1-2 MB.
</details>

<details>
<summary><strong>Can I use this with ChatGPT / Claude / Gemini / etc.?</strong></summary>

Yes. The `summary.md` output is plain Markdown — paste it into any LLM chat. The JSON files can be attached or referenced. AVIF screenshots can be included for models that support image input.
</details>

---

## License

[Blue Oak Model License 1.0.0](LICENSE) — a modern, readable, permissive open-source license.
