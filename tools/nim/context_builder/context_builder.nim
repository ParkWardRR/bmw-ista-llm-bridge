## ista-context: Compact LLM context builder for BMW ISTA diagnostic data.
##
## Reads session bundle JSONs (vehicle.json, faults.json, ecus.json,
## tests.json, timeline.json) and produces a token-efficient diagnostic
## context optimized for LLM consumption.
##
## Safety: read-only — no vehicle communication, only reads JSON files

import std/[json, os, strformat, strutils, algorithm, sequtils, parseopt]

const Version = "0.1.0"

# ---------------------------------------------------------------------------
# Types — mirror the bundle JSON schema from bundle.go
# ---------------------------------------------------------------------------

type
  Vehicle = object
    vin: string
    brand: string
    series: string
    modelSeries: string
    body: string
    engine: string
    transmission: string
    modelYear: string
    market: string
    steering: string
    assembly: string
    iLevel: string
    mileageKm: int
    commType: string

  Fault = object
    ecu: string
    ecuFullName: string
    bus: string
    code: int
    description: string
    status: string
    warning: string
    pCode: string
    mileageKm: int

  ECU = object
    name: string
    fullName: string
    variant: string
    bus: string
    protocol: string
    supplier: string
    faultCount: int
    commOK: bool
    softwareIDs: seq[string]

  TestResult = object
    name: string
    result: string
    detail: string

  TimelineEvent = object
    timestamp: string
    level: string
    source: string
    message: string

  ContextSection = object
    key: string
    label: string
    content: string
    tokens: int

  ContextOutput = object
    context: string
    tokens: int
    sections: seq[ContextSection]
    vehicleSummary: string
    faultCount: int
    activeFaults: int
    ecuCount: int
    testsPassed: int
    testsFailed: int
    sources: seq[string]

# ---------------------------------------------------------------------------
# JSON parsing
# ---------------------------------------------------------------------------

proc parseVehicle(path: string): Vehicle =
  if not fileExists(path): return
  let j = parseJson(readFile(path))
  result.vin = j.getOrDefault("vin").getStr
  result.brand = j.getOrDefault("brand").getStr
  result.series = j.getOrDefault("series").getStr
  result.modelSeries = j.getOrDefault("model_series").getStr
  result.body = j.getOrDefault("body").getStr
  result.engine = j.getOrDefault("engine").getStr
  result.transmission = j.getOrDefault("transmission").getStr
  result.modelYear = j.getOrDefault("model_year").getStr
  result.market = j.getOrDefault("market").getStr
  result.steering = j.getOrDefault("steering").getStr
  result.assembly = j.getOrDefault("assembly_country").getStr
  result.iLevel = j.getOrDefault("i_level").getStr
  result.mileageKm = j.getOrDefault("mileage_km").getInt
  result.commType = j.getOrDefault("comm_type").getStr

proc parseFaults(path: string): seq[Fault] =
  if not fileExists(path): return
  let j = parseJson(readFile(path))
  for item in j:
    var f: Fault
    f.ecu = item.getOrDefault("ecu").getStr
    f.ecuFullName = item.getOrDefault("ecu_full_name").getStr
    f.bus = item.getOrDefault("bus").getStr
    f.code = item.getOrDefault("code").getInt
    f.description = item.getOrDefault("description").getStr
    f.status = item.getOrDefault("status").getStr
    f.warning = item.getOrDefault("warning").getStr
    f.pCode = item.getOrDefault("p_code").getStr
    f.mileageKm = item.getOrDefault("mileage_km").getInt
    result.add(f)

proc parseECUs(path: string): seq[ECU] =
  if not fileExists(path): return
  let j = parseJson(readFile(path))
  for item in j:
    var e: ECU
    e.name = item.getOrDefault("name").getStr
    e.fullName = item.getOrDefault("full_name").getStr
    e.variant = item.getOrDefault("variant").getStr
    e.bus = item.getOrDefault("bus").getStr
    e.protocol = item.getOrDefault("protocol").getStr
    e.supplier = item.getOrDefault("supplier").getStr
    e.faultCount = item.getOrDefault("fault_count").getInt
    e.commOK = item.getOrDefault("comm_ok").getBool
    let ids = item.getOrDefault("software_ids")
    if ids != nil and ids.kind == JArray:
      for id in ids: e.softwareIDs.add(id.getStr)
    result.add(e)

proc parseTests(path: string): seq[TestResult] =
  if not fileExists(path): return
  let j = parseJson(readFile(path))
  for item in j:
    var t: TestResult
    t.name = item.getOrDefault("name").getStr
    t.result = item.getOrDefault("result").getStr
    t.detail = item.getOrDefault("detail").getStr
    result.add(t)

proc parseTimeline(path: string): seq[TimelineEvent] =
  if not fileExists(path): return
  let j = parseJson(readFile(path))
  for item in j:
    var e: TimelineEvent
    e.timestamp = item.getOrDefault("timestamp").getStr
    e.level = item.getOrDefault("level").getStr
    e.source = item.getOrDefault("source").getStr
    e.message = item.getOrDefault("message").getStr
    result.add(e)

# ---------------------------------------------------------------------------
# Token estimation
# ---------------------------------------------------------------------------

proc estimateTokens(s: string): int =
  ## ~4 chars per token for mixed English/technical content.
  ## Slightly generous — better to overestimate than surprise the user.
  (s.len + 3) div 4

# ---------------------------------------------------------------------------
# Compact rendering — every token counts
# ---------------------------------------------------------------------------

proc pad(s: string, width: int): string =
  if s.len >= width: s
  else: s & ' '.repeat(width - s.len)

proc renderVehicle(v: Vehicle): string =
  var model = v.series
  if v.modelSeries.len > 0 and v.modelSeries != v.series:
    model &= " " & v.modelSeries
  if v.engine.len > 0:
    model &= " " & v.engine
  if v.transmission.len > 0:
    model &= "/" & v.transmission

  result = &"# BMW {model} — {v.vin}\n"

  var meta: seq[string]
  if v.modelYear.len > 0: meta.add(v.modelYear)
  if v.mileageKm > 0: meta.add(&"{v.mileageKm} km")
  if v.iLevel.len > 0: meta.add(&"I-Level: {v.iLevel}")
  if v.commType.len > 0: meta.add(&"via {v.commType}")
  if v.market.len > 0: meta.add(v.market)
  if meta.len > 0:
    result &= meta.join(" | ") & "\n"

proc renderFaults(faults: seq[Fault]): string =
  if faults.len == 0:
    return "## No faults stored\n"

  var active, stored: seq[Fault]
  for f in faults:
    if f.status.toLowerAscii in ["present", "active", "confirmedDTC,testFailed"]:
      active.add(f)
    else:
      stored.add(f)

  result = &"## {faults.len} Faults"
  if active.len > 0:
    result &= &" ({active.len} active, {stored.len} stored)"
  result &= "\n"

  for f in active:
    let pc = if f.pCode.len > 0: f.pCode.pad(7) else: "—".pad(7)
    let desc = if f.description.len > 0: f.description
               else: &"code {f.code}"
    result &= &"ACTIVE  {f.ecu.pad(6)} {f.code:>08X}  {pc} {desc}\n"

  for f in stored:
    let pc = if f.pCode.len > 0: f.pCode.pad(7) else: "—".pad(7)
    let desc = if f.description.len > 0: f.description
               else: &"code {f.code}"
    result &= &"STORED  {f.ecu.pad(6)} {f.code:>08X}  {pc} {desc}\n"

proc renderECUsFaultsOnly(ecus: seq[ECU]): string =
  let withFaults = ecus.filterIt(it.faultCount > 0)
  let healthy = ecus.len - withFaults.len

  result = &"## ECUs: {withFaults.len}/{ecus.len} with faults\n"
  for e in withFaults:
    let supplier = if e.supplier.len > 0: e.supplier.pad(10) else: "—".pad(10)
    let variant = if e.variant.len > 0: e.variant else: "—"
    result &= &"{e.name.pad(6)} {e.bus.pad(6)} {supplier} {variant}  {e.faultCount} fault(s)\n"

  if healthy > 0:
    result &= &"[+{healthy} ECUs — all OK]\n"

proc renderECUsFull(ecus: seq[ECU]): string =
  result = &"## All {ecus.len} ECUs\n"

  let sorted = ecus.sortedByIt(-it.faultCount)
  for e in sorted:
    let supplier = if e.supplier.len > 0: e.supplier.pad(10) else: "—".pad(10)
    let variant = if e.variant.len > 0: e.variant else: "—"
    let note = if e.faultCount > 0: &"  {e.faultCount} fault(s)"
               elif not e.commOK: "  NO COMM"
               else: ""
    let sw = if e.softwareIDs.len > 0: e.softwareIDs[0] else: "—"
    result &= &"{e.name.pad(6)} {e.bus.pad(6)} {supplier} {variant.pad(16)} SW:{sw}{note}\n"

proc renderTests(tests: seq[TestResult]): string =
  if tests.len == 0: return ""

  var failed, passed, skipped: seq[TestResult]
  for t in tests:
    case t.result.toLowerAscii
    of "failed", "fail": failed.add(t)
    of "passed", "pass": passed.add(t)
    else: skipped.add(t)

  result = &"## Tests: {passed.len} passed, {failed.len} failed"
  if skipped.len > 0: result &= &", {skipped.len} skipped"
  result &= "\n"

  for t in failed:
    let detail = if t.detail.len > 0: "  " & t.detail else: ""
    result &= &"FAIL  {t.name}{detail}\n"

  if passed.len <= 5:
    for t in passed:
      let detail = if t.detail.len > 0: "  " & t.detail else: ""
      result &= &"PASS  {t.name}{detail}\n"
  else:
    for t in passed[0..2]:
      let detail = if t.detail.len > 0: "  " & t.detail else: ""
      result &= &"PASS  {t.name}{detail}\n"
    result &= &"[+{passed.len - 3} more passed]\n"

proc renderTimeline(events: seq[TimelineEvent]): string =
  if events.len == 0: return ""

  # Filter to important events — errors, warnings, and key lifecycle events
  var important: seq[TimelineEvent]
  let keywords = ["error", "fault", "fail", "connect", "disconnect", "identified",
                   "started", "complete", "abort", "vin", "i-level", "voltage",
                   "battery", "warning", "fasta"]

  for e in events:
    let msg = e.message.toLowerAscii
    let isError = e.level.toLowerAscii in ["error", "warn"]
    let isKey = keywords.anyIt(msg.contains(it))
    if isError or isKey:
      important.add(e)

  if important.len == 0:
    important = if events.len <= 10: events
                else: events[0..4] & events[^4..^0]

  let shown = if important.len <= 15: important
              else: important[0..7] & important[^6..^0]

  result = &"## Timeline ({events.len} events)\n"
  for e in shown:
    let ts = if e.timestamp.len > 8: e.timestamp[0..7] else: e.timestamp
    let lvl = if e.level.toLowerAscii in ["error", "warn"]: e.level.toUpperAscii & " " else: ""
    result &= &"{ts}  {lvl}{e.message}\n"

  if important.len > shown.len:
    result &= &"[+{important.len - shown.len} more events]\n"

# ---------------------------------------------------------------------------
# Context assembly
# ---------------------------------------------------------------------------

proc buildContext(dataDir: string, fullECUs: bool, budget: int): ContextOutput =
  let vehiclePath = dataDir / "vehicle.json"
  let faultsPath = dataDir / "faults.json"
  let ecusPath = dataDir / "ecus.json"
  let testsPath = dataDir / "tests.json"
  let timelinePath = dataDir / "timeline.json"

  let vehicle = parseVehicle(vehiclePath)
  let faults = parseFaults(faultsPath)
  let ecus = parseECUs(ecusPath)
  let tests = parseTests(testsPath)
  let timeline = parseTimeline(timelinePath)

  # Track sources
  var sources: seq[string]
  if fileExists(vehiclePath): sources.add("vehicle")
  if fileExists(faultsPath): sources.add("faults")
  if fileExists(ecusPath): sources.add("ecus")
  if fileExists(testsPath): sources.add("tests")
  if fileExists(timelinePath): sources.add("timeline")

  # Render sections
  let sVehicle = renderVehicle(vehicle)
  let sFaults = renderFaults(faults)
  let sECUs = if fullECUs: renderECUsFull(ecus)
              else: renderECUsFaultsOnly(ecus)
  let sTests = renderTests(tests)
  let sTimeline = renderTimeline(timeline)

  var sections: seq[ContextSection]
  sections.add(ContextSection(key: "vehicle", label: "Vehicle",
    content: sVehicle, tokens: estimateTokens(sVehicle)))
  sections.add(ContextSection(key: "faults",
    label: &"Faults ({faults.len})",
    content: sFaults, tokens: estimateTokens(sFaults)))

  let ecuLabel = if fullECUs: &"All ECUs ({ecus.len})"
                 else: &"ECUs with faults"
  sections.add(ContextSection(key: "ecus", label: ecuLabel,
    content: sECUs, tokens: estimateTokens(sECUs)))

  if tests.len > 0:
    sections.add(ContextSection(key: "tests",
      label: &"Tests ({tests.len})",
      content: sTests, tokens: estimateTokens(sTests)))

  if timeline.len > 0:
    sections.add(ContextSection(key: "timeline",
      label: &"Timeline ({timeline.len})",
      content: sTimeline, tokens: estimateTokens(sTimeline)))

  # Assemble full context
  var parts: seq[string]
  for s in sections:
    if s.content.len > 0:
      parts.add(s.content)

  let context = parts.join("\n")

  # Count faults
  var activeFaults = 0
  for f in faults:
    if f.status.toLowerAscii in ["present", "active"]:
      inc activeFaults

  # Count tests
  var testsPassed, testsFailed = 0
  for t in tests:
    case t.result.toLowerAscii
    of "passed", "pass": inc testsPassed
    of "failed", "fail": inc testsFailed
    else: discard

  # Vehicle summary line
  var vsummary = vehicle.series
  if vehicle.modelSeries.len > 0 and vehicle.modelSeries != vehicle.series:
    vsummary &= " " & vehicle.modelSeries
  vsummary &= " " & vehicle.vin

  result = ContextOutput(
    context: context,
    tokens: estimateTokens(context),
    sections: sections,
    vehicleSummary: vsummary,
    faultCount: faults.len,
    activeFaults: activeFaults,
    ecuCount: ecus.len,
    testsPassed: testsPassed,
    testsFailed: testsFailed,
    sources: sources,
  )

  # If budget specified and we're over, try trimming
  if budget > 0 and result.tokens > budget:
    # Re-render with compact ECUs
    if fullECUs:
      result = buildContext(dataDir, fullECUs = false, budget = budget)

# ---------------------------------------------------------------------------
# Output
# ---------------------------------------------------------------------------

proc outputPlain(ctx: ContextOutput) =
  stdout.write(ctx.context)
  stderr.writeLine(&"\n---\n~{ctx.tokens} tokens | {ctx.sources.join(\", \")} | ista-context {Version}")

proc outputJSON(ctx: ContextOutput) =
  var j = %* {
    "context": ctx.context,
    "tokens": ctx.tokens,
    "vehicle_summary": ctx.vehicleSummary,
    "fault_count": ctx.faultCount,
    "active_faults": ctx.activeFaults,
    "ecu_count": ctx.ecuCount,
    "tests_passed": ctx.testsPassed,
    "tests_failed": ctx.testsFailed,
    "sources": ctx.sources,
    "sections": newJArray(),
  }
  for s in ctx.sections:
    j["sections"].add(%* {
      "key": s.key,
      "label": s.label,
      "tokens": s.tokens,
    })
  stdout.writeLine(j.pretty)

# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------

proc usage() =
  stderr.writeLine """
ista-context — Compact LLM context builder for BMW ISTA diagnostic data

Usage:
  ista-context --data-dir <path> [options]

Options:
  --data-dir <path>    Session bundle directory (contains vehicle.json, etc.)
  --json               Structured JSON output (for orchestrator)
  --full-ecus          Include all ECUs, not just those with faults
  --budget <tokens>    Target token budget (auto-trims if over)
  --version            Show version
  --help               Show this help

The context output is optimized for LLM consumption — compact, structured,
every token earns its place. Pipe to clipboard or feed directly to your LLM.

Examples:
  ista-context --data-dir ./session_2026-09-21_WBAPH5C55BA123456/
  ista-context --data-dir ./session_.../ --json
  ista-context --data-dir ./session_.../ --budget 1500
  ista-context --data-dir ./session_.../ | pbcopy   # macOS clipboard
  ista-context --data-dir ./session_.../ | clip      # Windows clipboard
"""
  quit(0)

proc main() =
  var dataDir = ""
  var jsonOutput = false
  var fullECUs = false
  var budget = 0

  var p = initOptParser(commandLineParams())
  while true:
    p.next()
    case p.kind
    of cmdEnd: break
    of cmdShortOption, cmdLongOption:
      case p.key
      of "data-dir", "d": dataDir = p.val
      of "json": jsonOutput = true
      of "full-ecus": fullECUs = true
      of "budget", "b":
        budget = parseInt(p.val)
      of "version", "v":
        echo &"ista-context {Version}"
        quit(0)
      of "help", "h": usage()
      else:
        stderr.writeLine(&"Unknown option: {p.key}")
        quit(1)
    of cmdArgument:
      if dataDir.len == 0: dataDir = p.key

  if dataDir.len == 0:
    stderr.writeLine("Error: --data-dir is required")
    stderr.writeLine("Run with --help for usage")
    quit(1)

  if not dirExists(dataDir):
    stderr.writeLine(&"Error: directory not found: {dataDir}")
    quit(1)

  let ctx = buildContext(dataDir, fullECUs, budget)

  if ctx.sources.len == 0:
    stderr.writeLine(&"Error: no data files found in {dataDir}")
    stderr.writeLine("Expected: vehicle.json, faults.json, ecus.json, etc.")
    quit(1)

  if jsonOutput:
    outputJSON(ctx)
  else:
    outputPlain(ctx)

when isMainModule:
  main()
