# Ecosystem Integration Plan

How ista-bridge relates to the broader BMW open-source diagnostic ecosystem — what we absorb, what we contribute, and in what order. The bridge workflow is **gather → build context → copy to LLM** — every integration feeds into that pipeline.

> **Status:** Active — September 2026. Bridge workflow, 8 satellites, and TUI are all shipped.
>
> **Scope:** F-series (F20/F22/F25/F30/F32 etc.) and newer chassis over ENET/DoIP.
>
> **Language policy:** New code is written in **Nim** (preferred), Gleam, Zig, or Odin — **never Go**. Go is the thin orchestrator/TUI shell only. All logic, rendering, protocol work, and data processing goes into polyglot satellites.
>
> **Safety policy:** All vehicle communication is **strictly read-only**. Write UDS services (0x2E, 0x2F, 0x31, 0x34–0x37, 0x3D, 0x14) are hard-blocked at the protocol layer and never reach the wire. No flashing, no coding, no DTC clearing, no actuator control.

---

## At a glance

```
                           ┌─────────────────────┐
                           │  ista-bridge (Go)    │
                           │  thin orchestrator   │
                           │  TUI shell, Win32    │
                           └────┬────────┬────────┘
                                │        │
               ┌────────────────┘        └────────────────┐
               ▼                                          ▼
  ┌──────────────────────┐              ┌──────────────────────────┐
  │   SATELLITES (Nim)   │              │   SATELLITES (others)    │
  │                      │              │                          │
  │  ista-context ← brain│              │  ista-faultlookup (Gleam)│
  │  ista-enet   ← car   │              │  ista-vinlookup   (Zig)  │
  │  ista-import ← files │              │  ista-keyextract  (Odin) │
  │  ista-report ← docs  │              │                          │
  └──────────┬───────────┘              └──────────────────────────┘
             │
             ▼
  ┌──────────────────────┐
  │  LLM Context Output  │
  │                      │
  │  [c] clipboard       │
  │  [w] context.txt     │
  │  [p] stdout (pipe)   │
  │                      │
  │  ~800-1200 tokens    │
  │  per session         │
  └──────────────────────┘

BRIDGE WORKFLOW: [Enter] Gather All → toggle sections → [c/w/p] → paste into LLM

SAFETY: write UDS services are hard-blocked at the protocol layer.
        The car is READ-ONLY. Always.
```

---

## Part 1 — What we take

### 1. klartext (Rust) — Native F-series diagnostics over ENET

**Priority: High** — unblocks direct vehicle communication, the biggest gap in ista-bridge today.

| What to take | How it helps us | Integration approach |
|---|---|---|
| HSFZ/ENET transport layer | Removes the "blocked" DoIP line from our roadmap — we can talk to the car without ISTA running | **Rewritten in Nim** as `ista-enet` satellite — own HSFZ implementation based on klartext's protocol approach, not a Rust dependency |
| UDS diagnostic layer | Read fault codes, read live data directly | `ista-enet` implements read-only UDS subset. **Write services are hard-blocked at the protocol layer — they never reach the wire.** |
| ISTA-derived semantic lookup | klartext already maps raw UDS responses to human-readable ISTA names | Feed klartext's lookup tables into our Gleam fault-lookup satellite or merge into our DiagDocDb exports |
| Native MCP surface | klartext already speaks MCP — an LLM agent can drive diagnostics | Bridge klartext's MCP tools into our planned Phase 4 LLM integration so `ista-bridge ask` can do live reads |

**Status: ista-enet satellite BUILT** (`polyglot/nim/enet_client/enet_client.nim`)

**What was done:**
1. ~~Add klartext as a git submodule~~ → Rewrote HSFZ/UDS protocol in Nim (`ista-enet`), no Rust dependency
2. Registered `enet` satellite in `orchestrate.go` with discovery pattern
3. New `live` command group: `ista-enet faults`, `ista-enet ecus`, `ista-enet data`, `ista-enet vin` — JSON output for Go orchestrator
4. Read-only safety: `enforceReadOnly()` rejects blocked UDS services before they're serialized
5. Makefile target: `make nim-enet`

**Remaining:**
- Merge klartext's SGBD-derived lookup tables with our `db export-lookup` JSON so fault searches hit both sources
- Wire `ista-bridge live` Go CLI commands to shell out to `ista-enet` (TUI already has a Live view)

### 2. headless-ista — MCP agent for live ISTA

**Priority: High** — automates the manual ISTA workflow that ista-bridge currently just observes.

| What to take | How it helps us | Integration approach |
|---|---|---|
| Programmatic ISTA navigation | Automate "connect → read faults → run FASTA → disconnect" instead of requiring the user to click through ISTA | Call headless-ista's MCP tools from our Phase 4 LLM integration |
| Session triggering | Let `ista-bridge ask` trigger a fresh diagnostic session when the LLM needs more data | MCP tool call → headless-ista navigates ISTA → ista-bridge captures the resulting session artifacts |
| State-change gating model | headless-ista requires approval before mutating actions — our approach is stricter: **we never allow mutating actions at all** | No approval gate needed — write operations are blocked at the protocol layer in `ista-enet`. The car is always read-only. |

**Concrete steps:**
1. Document headless-ista as the recommended companion for automated workflows
2. In Phase 4 (`ista-bridge ask`), add an MCP client that can call headless-ista tools
3. Build an `auto-session` command: headless-ista drives ISTA through a full read-only diagnostic pass, ista-bridge captures and bundles the result
4. ~~Adopt headless-ista's approval-gate pattern~~ → Not needed. ista-enet enforces read-only at the protocol layer. There is no gate because there is no write path.

### 3. EDIABASLib / Deep OBD — Open EDIABAS interpreter

**Priority: Medium** — the most mature project in the ecosystem; provides the EDIABAS job execution layer.

| What to take | How it helps us | Integration approach |
|---|---|---|
| BMW-FAST-ENET protocol implementation | A battle-tested F-series transport stack we could use alongside or instead of klartext | EDIABASLib is C#/.NET — call via our existing PowerShell bridge pattern, or use Deep OBD's Android app as a reference implementation |
| EDIABAS job definitions and execution | Execute specific diagnostic jobs (read adaptation values, read coding data) that ISTA runs internally | Import job definitions to enrich our ECUKom parsing — we currently extract EDIABAS results from XML but don't know what jobs to request |
| SGBD/PRG file parsing | Understand the binary ECU description files that drive ISTA's diagnostic logic | Use parsed SGBD data to add ECU-specific context to our bundles (what parameters each ECU exposes, what jobs are available) |

**Concrete steps:**
1. Study EDIABASLib's `EdiabasLib/EdiabasLib/EdInterfaceEnet.cs` for the BMW-FAST/ENET protocol details
2. Extract EDIABAS job name → human description mappings and add to our fault/lookup data exports
3. Evaluate whether EDIABASLib's .NET dependency is acceptable or if klartext's Rust reimplementation is preferable for our use case
4. If EDIABASLib is used: add a new satellite slot `ista-ediabas` with PowerShell bridge fallback

### 4. BMWeb — Browser-based INPA/EDIABAS diagnostics

**Priority: Medium** — explicit F30 coverage and a complementary data format.

| What to take | How it helps us | Integration approach |
|---|---|---|
| F30 chassis definitions | BMWeb has explicit F30 support — we can validate our DiagDocDb coverage against their module lists | Cross-reference BMWeb's supported-ECU list with our VINRANGES and XEP_FAULTCODES tables |
| JSON scan export format | BMWeb exports scans as JSON — if users run both tools, we can import BMWeb scans into our bundles | Add an `import bmweb <file>` command that merges a BMWeb JSON export into an ista-bridge session bundle |
| Workshop data / diagnostic procedures | BMWeb includes procedure-level diagnostic guidance | Extract and include in our LLM bundles as additional context for the AI |

**Status: importer BUILT** (`polyglot/nim/data_import/data_import.nim`)

**What was done:**
1. ~~Write an importer in Go~~ → Written in **Nim** as `ista-import` satellite
2. `ista-import --bmweb <scan.json>` normalizes BMWeb data into bundle format (vehicle.json, faults.json, ecus.json)
3. Registered `import` satellite in `orchestrate.go`, Makefile target: `make nim-import`

**Remaining:**
- Cross-reference F30 ECU/module coverage: identify any ECUs BMWeb covers that our DiagDocDb exports miss

### 5. Beemuu — Desktop diagnostic suite

**Priority: Medium** — strong file formats for LLM consumption and community data.

| What to take | How it helps us | Integration approach |
|---|---|---|
| Snapshot JSON format | Beemuu's snapshot files are already structured for replay/analysis | Write an importer: `ista-bridge import --beemuu <snapshot.json>` |
| CSV live-data logs | Time-series sensor data that our bundles currently lack | Add a `live-data` section to our bundle format — include CSV alongside our existing JSON files |
| Community diagnostic knowledge | Beemuu's community features may surface common fault patterns | If Beemuu exposes an API or export, pull community fix data into our LLM prompt context |

**Status: importer BUILT** (shared `ista-import` Nim satellite)

**What was done:**
1. Beemuu snapshot import implemented in `ista-import --beemuu <snapshot.json>`
2. Maps Beemuu vehicle/ECU/DTC fields to bundle format with `data_source: "beemuu"` tagging

**Remaining:**
- Add `livedata.csv` support to the bundle format (new section in summary.md)

### 6. BimmerDis + BimmerJson — EDIABAS definition decoders

**Priority: Medium-Low** — offline enrichment pipeline, not a runtime dependency.

| What to take | How it helps us | Integration approach |
|---|---|---|
| Structured JSON from EDIABAS telegram tables | Decode the binary .prg/.grp files that define ECU communication into human-readable JSON | Run BimmerJson as a build-time pipeline: .prg → JSON → include in our data exports |
| ECU communication definitions | Know what parameters each ECU exposes, what jobs are available, what the response fields mean | Merge into our DiagDocDb exports so fault lookups include the underlying EDIABAS context |

**Concrete steps:**
1. Run BimmerDis + BimmerJson against the F-series EDIABAS files that ship with ISTA
2. Store the resulting JSON alongside our `db export-lookup` output
3. Enrich our `ecus.json` bundle output with per-ECU job/parameter lists from the decoded definitions

### 7. rawenet — ENET gateway/proxy

**Priority: Low** — infrastructure utility, useful once we have direct vehicle communication.

| What to take | How it helps us | Integration approach |
|---|---|---|
| HSFZ transport implementation | Reference implementation for the framing layer klartext also uses | Use as a test fixture: run rawenet as a proxy between ista-bridge and the car to capture/replay raw ENET traffic |
| Remote diagnostic capability | rawenet forwards ENET over the network — enables diagnosing a car from a different machine | Document as an optional companion for remote setups |

**Concrete steps:**
1. Use rawenet during development to capture ENET traces for protocol testing
2. Document the `rawenet → car` setup for users who want remote diagnostics

### 8. svietlik (TypeScript) — BMW lighting control over ENET/UDS

**Priority: Low-Medium** — narrow scope (lighting only), but its HSFZ/UDS implementation and module-scanning approach are directly reusable.

svietlik is a TypeScript library and CLI that talks to BMW light modules (FEM, FLE02, REM) over ENET using HSFZ framing and UDS diagnostic services. Tested on F82 M4, expected to work on other F-series with the same module names. G-series connects via DoIP but light commands are unmapped.

| What to take | How it helps us | Integration approach |
|---|---|---|
| HSFZ + UDS implementation in TypeScript | A clean, modern reference for the same protocol stack klartext implements in Rust — useful for cross-validating our understanding | Study alongside klartext's implementation; use as a second opinion on HSFZ framing edge cases |
| Module scanning and discovery | svietlik scans for available ECUs and reports their identifiers, variants, and compatibility — similar to what our `live ecus` command needs | Adapt the scan approach for our klartext satellite integration: scan → identify modules → read status |
| JSON scan report format | Produces structured JSON with module inventory, VIN (masked), battery voltage | Import as a lightweight data source: `ista-bridge import --svietlik <scan.json>` to add module-level detail to a session bundle |
| Show/sequence JSON format | Defines timed command sequences as JSON — a pattern we could reuse for scripted diagnostic sequences | Reference for designing our own diagnostic sequence format if we ever support scripted multi-step reads |
| React Native socket layer | Custom TCP socket abstraction that works cross-platform including mobile | Reference if we ever consider a mobile companion app |

**Status: partially BUILT**

**What was done:**
1. svietlik's HSFZ framing was studied and reimplemented in Nim (`ista-enet`) — protocol verified against both svietlik (TS) and klartext (Rust) approaches
2. Module-scan logic adapted for `ista-enet ecus` command
3. `ista-import --svietlik <scan.json>` built — ingests module inventory into bundle format

**Remaining:**
- If svietlik adds fault-reading or broader diagnostic features, revisit as additional import source

### 9. pydiabas — Python EDIABAS wrapper

**Priority: Low** — useful for prototyping, not for production integration.

| What to take | How it helps us | Integration approach |
|---|---|---|
| Simulation mode | pydiabas can simulate EDIABAS responses without a car — useful for testing our parsers | Use in CI: generate synthetic ECUKom XML from pydiabas simulation for our `ziplog_test.go` tests |
| Python adapter pattern | Shows how to wrap EDIABAS for a high-level language | Reference only — our Go + satellite architecture already fills this role |

**Concrete steps:**
1. Evaluate pydiabas's simulation mode for generating test fixtures
2. If viable, add a `make test-fixtures` target that uses pydiabas to generate synthetic EDIABAS data

---

## Part 2 — What we give back

ista-bridge has invested heavily in areas that most of these projects haven't touched. These capabilities are ready to share.

### DiagDocDb decryption and query layer

**Beneficiaries:** klartext, BMWeb, Beemuu, EDIABASLib/Deep OBD, svietlik

Our `db.go` + `ista-keyextract` (Odin) pipeline solves a problem every project faces: the 7 GB encrypted DiagDocDb is the single richest data source in ISTA, but nobody else has cracked it open programmatically.

| What we offer | Form factor |
|---|---|
| Encryption key extraction | `ista-keyextract` binary or the algorithm (SHA-1 of .NET assembly public key → uppercase hex token) |
| Pre-exported lookup tables | `db export-lookup` produces JSON files any project can consume without needing 32-bit PowerShell |
| Raw query interface | `db query <sql>` for ad-hoc exploration of 232 tables |
| VIN range data | 7.9M VINRANGES records exported as JSON — model identification without a car connection |
| Fault code enrichment | XEP_FAULTCODES + CODEASSIGNMENT tables with full descriptions, P-codes, ECU mappings |

**How to share:**
1. Publish `db export-lookup` JSON files as a standalone downloadable dataset (version-tagged by ISTA release)
2. Document the decryption algorithm so other projects can implement their own access
3. Offer `ista-keyextract` as a standalone tool (it already works independently)

### LLM-ready bundle format

**Beneficiaries:** All projects that want LLM integration

Our bundle format (`vehicle.json`, `ecus.json`, `faults.json`, `ecukom.json`, `tests.json`, `timeline.json`, `summary.md`) is the most complete LLM-oriented packaging of BMW diagnostic data that exists.

| What we offer | Form factor |
|---|---|
| Bundle specification | Document the JSON schemas so other tools can produce compatible bundles |
| Report generation | `ista-report` (Nim) can render any project's data if they output our JSON format |
| Prompt templates | Our Phase 4 prompt templates (when built) will work with any bundle-format data |
| Token estimation | Our planned token-count estimator helps any project right-size data for LLM context windows |

**How to share:**
1. Publish the bundle JSON schemas as a specification document
2. Package `ista-report` (Nim) as a standalone tool that any project can invoke
3. Accept contributions to the bundle format from other projects (new section types, additional metadata)

### Session parsing and correlation

**Beneficiaries:** headless-ista, any project that reads ISTA artifacts

Our session discovery engine (`session.go`) is the only implementation that correlates all six ISTA data sources (META, TRANS, PRG, behdat, fstdat, zip.log) into a unified session.

| What we offer | Form factor |
|---|---|
| Session discovery algorithm | The pattern-matching and timestamp-correlation logic for finding related ISTA artifacts |
| zip.log extraction | ECUKom XML parsing and IstaOperation.log timeline extraction |
| FASTA parsing | Bilingual (DE/EN) test result normalization |

**How to share:**
1. Extract the session-parsing logic into a reusable Go library (separate package within this repo)
2. Document the ISTA artifact file layout and naming conventions so other projects can implement their own parsers

### Screenshot capture pipeline

**Beneficiaries:** headless-ista, any UI-automation project

Our Win32 capture → pHash → pixel diff → AVIF pipeline is production-grade and GPU-accelerated.

| What we offer | Form factor |
|---|---|
| Change-detection algorithm | pHash pre-filter + 64-bit block pixel comparison with alpha masking |
| GPU encoder probing | QSV → NVENC → AMF → CPU fallback chain for AVIF encoding |
| Base64 screenshot embedding | `ista-report` handles inlining screenshots into HTML reports |

**How to share:**
1. Document the capture pipeline architecture for reference
2. The approach is Win32-specific but the change-detection algorithm is portable

### Module and ECU enrichment for svietlik

**Beneficiary:** svietlik

svietlik currently hard-codes its supported module list (FEM_20, FLE02, REM_20) and has no ECU description database. We can dramatically expand its coverage.

| What we offer | Form factor |
|---|---|
| ECU/module definitions from DiagDocDb | Export the module tables (XEP_ECUS, XEP_VARIANTS) as JSON so svietlik can discover which light modules exist on any chassis, not just the ones manually tested |
| VIN-to-chassis mapping | Our 7.9M VINRANGES let svietlik auto-detect the chassis from a VIN and load the right module set |
| SGBD identifiers | Our session parsing extracts the SGBD for every ECU — svietlik could use these to know which diagnostic addresses and services each light module supports |
| F-series coverage validation | Our DiagDocDb queries can answer "which F-series chassis have FEM vs. BDC vs. LCM" — helping svietlik map its support matrix without needing one of every car |

**How to share:**
1. Run `db export` on the ECU-related tables and publish as a JSON dataset svietlik can bundle
2. Provide a VIN → light-module-list lookup query that svietlik can call or pre-compute

---

## Part 3 — Integration priorities

Ordered by impact on the F22/F30 diagnostic workflow:

| Priority | Integration | Status | Why | Unblocks |
|---|---|---|---|---|
| **P0** | klartext satellite | **DONE** — reimplemented in Nim as `ista-enet` | Direct vehicle communication without ISTA | TUI Live view, Phase 4 LLM-driven diagnosis |
| **P0** | DiagDocDb data publishing | **DONE** — `db export-lookup` ships JSON datasets | Other projects need this data and we have it | Community adoption, cross-project data sharing |
| **P0** | LLM bridge workflow | **DONE** — `ista-context` + bridge TUI | Gather → build context → copy to LLM in 2 keypresses | Phase 4 `ista-bridge ask` |
| **P1** | headless-ista companion | Planned | Automates the manual ISTA workflow | `auto-session`, Phase 4 `ista-bridge ask` with live data |
| **P1** | Bundle format specification | Planned | Formalizes output for ecosystem standardization | Cross-project LLM integration |
| **P1** | BMWeb/Beemuu/svietlik import | **DONE** — `ista-import` Nim satellite | Multi-source bundles with data provenance | Broader chassis coverage |
| **P2** | EDIABASLib job definitions | Planned | Enriches data with EDIABAS job context | Better ECUKom parsing, richer bundles |
| **P2** | BimmerDis/BimmerJson pipeline | Planned | Decodes binary ECU definitions | Per-ECU parameter lists, richer LLM context |
| **P3** | rawenet for remote diagnostics | Planned | Remote vehicle access | Remote/shop setups |
| **P3** | pydiabas test fixtures | Planned | Synthetic EDIABAS data for CI | Test coverage without a car |

---

## Part 4 — Safety and scope boundaries

All integrations must respect the project's absolute read-only posture:

1. **Read-only. Always. No exceptions.** There is no approval gate because there is no write path. The `ista-enet` satellite hard-blocks write UDS services (0x2E WriteDataByIdentifier, 0x2F InputOutputControlByIdentifier, 0x31 RoutineControl, 0x34–0x37 flash/programming, 0x3D WriteMemoryByAddress, 0x14 ClearDiagnosticInformation, 0x28 CommunicationControl, 0x85 ControlDTCSetting) at the protocol layer — they raise `SafetyError` before being serialized to the wire. This is not a policy that can be overridden by configuration or LLM request.

2. **No flashing, no programming, no DTC clearing, no coding, no adaptation, no actuator control.** ista-bridge is a diagnostic observation and query tool. It reads from the car. It never writes to the car.

3. **Offline-capable.** Every integration should work without a car connected. Live features (ista-enet) are additive — the core bundle/parse/query workflow must never require a vehicle connection.

4. **Data provenance.** When a bundle includes data from multiple sources (ista-bridge session + BMWeb scan + ista-enet live read), each data point is tagged with its `data_source` field so the LLM knows what it's looking at. The `ista-import` tool sets this automatically.

---

## Part 5 — New commands and TUI views

Summary of commands and views these integrations would add:

### CLI commands — satellite tools (Nim, not Go)

```
# ista-context (Nim) — the bridge brain [BUILT]
ista-context --data-dir ./session_.../ [--json]     # Compact LLM context (~800-1200 tok)
ista-context --data-dir ./session_.../ --budget 1500 # Auto-trim to token budget
ista-context --data-dir ./session_.../ --full-ecus   # Include all ECUs, not just faulted

# ista-enet (Nim) — read-only vehicle diagnostics [BUILT]
ista-enet faults [--ecu 0x00] [--json]   # Read DTCs (car required)
ista-enet ecus [--json]                   # Scan for available ECUs
ista-enet data --ecu 0x00 --did 0xF190   # Read a specific DID
ista-enet vin [--json]                    # Read VIN from ECU

# ista-import (Nim) — multi-format data importer [BUILT]
ista-import --bmweb <scan.json> [--out dir]      # Import BMWeb scan
ista-import --beemuu <snapshot.json> [--out dir]  # Import Beemuu snapshot
ista-import --svietlik <scan.json> [--out dir]    # Import svietlik scan
ista-import --klartext <dump.json> [--out dir]    # Import klartext dump
ista-import --auto <file.json> [--out dir]        # Auto-detect format
ista-import --bmweb a.json --beemuu b.json        # Merge multiple sources

# Go orchestrator pass-through
ista-bridge live faults              # Shells out to ista-enet [TUI BUILT, CLI planned]
ista-bridge live ecus                # Shells out to ista-enet [TUI BUILT, CLI planned]
ista-bridge import --bmweb <file>    # Shells out to ista-import [TUI BUILT, CLI planned]
ista-bridge auto-session             # headless-ista drives ISTA, we capture [planned]
ista-bridge ask "what's wrong?"      # Bundle + send to Claude/ChatGPT API [planned]
ista-bridge export --bundle-schema   # Export bundle JSON schemas [planned]
```

### TUI views

```
BUILT:
  Bridge      — [Enter] Gather All → build context → copy/save/pipe to LLM
  Context     — per-section token estimates, toggleable sections, live preview
  Gather      — progress bars for each data source, ista-context (Nim) status
  Sessions    — interactive table with cursor navigation, bundle from selection
  Live        — real-time fault/ECU/VIN via ista-enet (read-only safety banner)
  Import      — path input for BMWeb/Beemuu/svietlik files, auto-format detection
  Tools       — secondary features menu (ENET, import)

PLANNED:
  Multi-Source — side-by-side comparison when a session has data from multiple tools
```

---

## Appendix: Project compatibility matrix

Which projects work with which chassis and communication protocols:

| Project | F20 | F22 | F25 | F30 | F32 | ENET | DoIP | ICOM | OBD | MCP |
|---|---|---|---|---|---|---|---|---|---|---|
| ista-bridge | via ISTA | via ISTA | via ISTA | via ISTA | via ISTA | via ISTA | via ISTA | via ISTA | via ISTA | planned |
| klartext | dev | needs validation | wire-checked | needs validation | needs validation | yes | no (TODO) | no | no | yes |
| headless-ista | via ISTA | via ISTA | via ISTA | via ISTA | via ISTA | via ISTA | via ISTA | via ISTA | via ISTA | yes |
| BMWeb | ? | ? | ? | yes | ? | yes | ? | ? | ? | no |
| Beemuu | claimed | claimed | claimed | claimed | claimed | yes | yes | ? | ? | no |
| EDIABASLib | yes | yes | yes | yes | yes | yes | ? | yes | yes | no |
| rawenet | yes | yes | yes | yes | yes | yes | no (TODO) | no | no | no |
| svietlik | ? | ? | ? | ? | tested (F82) | yes | partial (G-series) | no | no | no |
| pydiabas | config-dependent | config-dependent | config-dependent | config-dependent | config-dependent | no | no | no | yes | no |
| BimmerDis | offline | offline | offline | offline | offline | n/a | n/a | n/a | n/a | no |
