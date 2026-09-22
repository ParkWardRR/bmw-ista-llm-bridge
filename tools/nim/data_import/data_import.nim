import std/[json, os, strutils, strformat, parseopt, times, tables, algorithm]

# ista-import: Multi-format data importer for ista-bridge.
#
# Ingests diagnostic scan data from BMWeb, Beemuu, and svietlik,
# normalizes it into the ista-bridge bundle format (vehicle.json,
# faults.json, ecus.json), and writes the result.
#
# This is a READ-ONLY pipeline — it transforms exported data files,
# never contacts the car.
#
# Rewritten in Nim from the Go integration plan. Based on the JSON
# export schemas described by each project but not copied from their
# codebases.

# ---------------------------------------------------------------------------
# Bundle output types (matches ista-bridge bundle format)
# ---------------------------------------------------------------------------
type
  BundleVehicle = object
    vin: string
    brand: string
    series: string
    modelSeries: string
    body: string
    engine: string
    transmission: string
    modelYear: string
    market: string
    iLevel: string
    commType: string
    mileageKm: int
    source: string

  BundleFault = object
    ecu: string
    code: int
    description: string
    status: string
    pCode: string
    safetyRelevant: bool
    source: string

  BundleEcu = object
    name: string
    fullName: string
    variant: string
    address: int
    bus: string
    commOk: bool
    hwVersion: string
    swVersion: string
    supplier: string
    serial: string
    source: string

  BundleData = object
    vehicle: BundleVehicle
    faults: seq[BundleFault]
    ecus: seq[BundleEcu]
    importSource: string
    importTime: string

# ---------------------------------------------------------------------------
# BMWeb JSON import
#
# BMWeb exports F30 diagnostic scans as JSON with structure:
#   { "vehicle": {...}, "modules": [...], "faults": [...], "scan_time": "..." }
# ---------------------------------------------------------------------------
proc importBmweb(path: string): BundleData =
  let raw = readFile(path)
  let j = parseJson(raw)

  result.importSource = "bmweb"
  result.importTime = now().utc.format("yyyy-MM-dd'T'HH:mm:ss'Z'")

  if j.hasKey("vehicle"):
    let v = j["vehicle"]
    result.vehicle = BundleVehicle(
      vin: v.getOrDefault("vin").getStr(""),
      brand: "BMW",
      series: v.getOrDefault("series").getStr(""),
      modelSeries: v.getOrDefault("model_series").getStr(""),
      body: v.getOrDefault("body_type").getStr(""),
      engine: v.getOrDefault("engine").getStr(""),
      transmission: v.getOrDefault("transmission").getStr(""),
      modelYear: v.getOrDefault("model_year").getStr(""),
      market: v.getOrDefault("market").getStr(""),
      iLevel: v.getOrDefault("i_level").getStr(""),
      commType: "ENET",
      mileageKm: v.getOrDefault("mileage_km").getInt(0),
      source: "bmweb",
    )

  if j.hasKey("modules"):
    for m in j["modules"]:
      result.ecus.add BundleEcu(
        name: m.getOrDefault("name").getStr(""),
        fullName: m.getOrDefault("full_name").getStr(m.getOrDefault("name").getStr("")),
        variant: m.getOrDefault("variant").getStr(""),
        address: m.getOrDefault("address").getInt(0),
        bus: m.getOrDefault("bus").getStr(""),
        commOk: m.getOrDefault("comm_ok").getBool(true),
        hwVersion: m.getOrDefault("hw_version").getStr(""),
        swVersion: m.getOrDefault("sw_version").getStr(""),
        supplier: m.getOrDefault("supplier").getStr(""),
        serial: m.getOrDefault("serial").getStr(""),
        source: "bmweb",
      )

  if j.hasKey("faults"):
    for f in j["faults"]:
      result.faults.add BundleFault(
        ecu: f.getOrDefault("module").getStr(f.getOrDefault("ecu").getStr("")),
        code: f.getOrDefault("code").getInt(0),
        description: f.getOrDefault("description").getStr(f.getOrDefault("text").getStr("")),
        status: f.getOrDefault("status").getStr("stored"),
        pCode: f.getOrDefault("p_code").getStr(f.getOrDefault("sae_code").getStr("")),
        safetyRelevant: f.getOrDefault("safety_relevant").getBool(false),
        source: "bmweb",
      )

# ---------------------------------------------------------------------------
# Beemuu snapshot import
#
# Beemuu exports snapshots as JSON with structure:
#   { "snapshot": { "vehicle": {...}, "ecus": [...], "dtcs": [...] },
#     "live_data": [...], "timestamp": "..." }
# ---------------------------------------------------------------------------
proc importBeemuu(path: string): BundleData =
  let raw = readFile(path)
  let j = parseJson(raw)

  result.importSource = "beemuu"
  result.importTime = now().utc.format("yyyy-MM-dd'T'HH:mm:ss'Z'")

  let snap = if j.hasKey("snapshot"): j["snapshot"] else: j

  if snap.hasKey("vehicle"):
    let v = snap["vehicle"]
    result.vehicle = BundleVehicle(
      vin: v.getOrDefault("vin").getStr(""),
      brand: "BMW",
      series: v.getOrDefault("series").getStr(""),
      modelSeries: v.getOrDefault("model").getStr(""),
      body: v.getOrDefault("body").getStr(""),
      engine: v.getOrDefault("engine_code").getStr(v.getOrDefault("engine").getStr("")),
      transmission: v.getOrDefault("transmission").getStr(""),
      modelYear: v.getOrDefault("year").getStr(""),
      market: v.getOrDefault("market").getStr(""),
      iLevel: v.getOrDefault("i_level").getStr(""),
      commType: v.getOrDefault("connection_type").getStr(""),
      mileageKm: v.getOrDefault("mileage").getInt(0),
      source: "beemuu",
    )

  if snap.hasKey("ecus"):
    for e in snap["ecus"]:
      result.ecus.add BundleEcu(
        name: e.getOrDefault("short_name").getStr(e.getOrDefault("name").getStr("")),
        fullName: e.getOrDefault("name").getStr(""),
        variant: e.getOrDefault("variant").getStr(""),
        address: e.getOrDefault("diag_address").getInt(e.getOrDefault("address").getInt(0)),
        bus: e.getOrDefault("bus").getStr(""),
        commOk: e.getOrDefault("responding").getBool(true),
        hwVersion: e.getOrDefault("hw_version").getStr(""),
        swVersion: e.getOrDefault("sw_version").getStr(""),
        supplier: e.getOrDefault("supplier").getStr(""),
        serial: e.getOrDefault("serial_number").getStr(""),
        source: "beemuu",
      )

  let dtcKey = if snap.hasKey("dtcs"): "dtcs"
               elif snap.hasKey("faults"): "faults"
               else: ""
  if dtcKey.len > 0:
    for f in snap[dtcKey]:
      result.faults.add BundleFault(
        ecu: f.getOrDefault("ecu").getStr(f.getOrDefault("module").getStr("")),
        code: f.getOrDefault("code").getInt(f.getOrDefault("dtc_code").getInt(0)),
        description: f.getOrDefault("description").getStr(f.getOrDefault("text").getStr("")),
        status: f.getOrDefault("status").getStr("stored"),
        pCode: f.getOrDefault("p_code").getStr(""),
        safetyRelevant: f.getOrDefault("safety").getBool(false),
        source: "beemuu",
      )

# ---------------------------------------------------------------------------
# svietlik module scan import
#
# svietlik exports module scans as JSON with structure:
#   { "vin": "WBA...", "battery_voltage": 12.4,
#     "modules": [ { "name": "FEM_20", "id": "...", "variant": "...",
#                     "compatible": true } ] }
# ---------------------------------------------------------------------------
proc importSvietlik(path: string): BundleData =
  let raw = readFile(path)
  let j = parseJson(raw)

  result.importSource = "svietlik"
  result.importTime = now().utc.format("yyyy-MM-dd'T'HH:mm:ss'Z'")

  result.vehicle = BundleVehicle(
    vin: j.getOrDefault("vin").getStr(""),
    brand: "BMW",
    commType: "ENET",
    source: "svietlik",
  )

  if j.hasKey("modules"):
    for m in j["modules"]:
      let moduleName = m.getOrDefault("name").getStr("")
      result.ecus.add BundleEcu(
        name: moduleName,
        fullName: m.getOrDefault("full_name").getStr(moduleName),
        variant: m.getOrDefault("variant").getStr(""),
        address: m.getOrDefault("diag_address").getInt(m.getOrDefault("id").getInt(0)),
        bus: "ENET",
        commOk: m.getOrDefault("compatible").getBool(m.getOrDefault("responding").getBool(true)),
        hwVersion: m.getOrDefault("hw_ref").getStr(""),
        swVersion: m.getOrDefault("sw_ref").getStr(""),
        source: "svietlik",
      )

  # svietlik is lighting-focused and doesn't typically export faults,
  # but handle it if present
  if j.hasKey("faults") or j.hasKey("dtcs"):
    let key = if j.hasKey("faults"): "faults" else: "dtcs"
    for f in j[key]:
      result.faults.add BundleFault(
        ecu: f.getOrDefault("module").getStr(""),
        code: f.getOrDefault("code").getInt(0),
        description: f.getOrDefault("description").getStr(""),
        status: f.getOrDefault("status").getStr("stored"),
        source: "svietlik",
      )

# ---------------------------------------------------------------------------
# klartext live-read import
#
# klartext can dump its diagnostic results as JSON:
#   { "vin": "...", "ecus": [...], "faults": [...] }
# ---------------------------------------------------------------------------
proc importKlartext(path: string): BundleData =
  let raw = readFile(path)
  let j = parseJson(raw)

  result.importSource = "klartext"
  result.importTime = now().utc.format("yyyy-MM-dd'T'HH:mm:ss'Z'")

  result.vehicle = BundleVehicle(
    vin: j.getOrDefault("vin").getStr(""),
    brand: "BMW",
    commType: "ENET",
    source: "klartext",
  )

  if j.hasKey("ecus"):
    for e in j["ecus"]:
      result.ecus.add BundleEcu(
        name: e.getOrDefault("name").getStr(""),
        fullName: e.getOrDefault("description").getStr(""),
        variant: e.getOrDefault("variant").getStr(""),
        address: e.getOrDefault("address").getInt(0),
        bus: e.getOrDefault("bus").getStr("ENET"),
        commOk: true,
        hwVersion: e.getOrDefault("hw_version").getStr(""),
        swVersion: e.getOrDefault("sw_version").getStr(""),
        supplier: e.getOrDefault("supplier").getStr(""),
        source: "klartext",
      )

  if j.hasKey("faults"):
    for f in j["faults"]:
      result.faults.add BundleFault(
        ecu: f.getOrDefault("ecu").getStr(""),
        code: f.getOrDefault("code").getInt(0),
        description: f.getOrDefault("description").getStr(""),
        status: f.getOrDefault("status").getStr("stored"),
        pCode: f.getOrDefault("p_code").getStr(""),
        safetyRelevant: f.getOrDefault("safety_relevant").getBool(false),
        source: "klartext",
      )

# ---------------------------------------------------------------------------
# Merge: combine multiple imports into one bundle
# ---------------------------------------------------------------------------
proc merge(bundles: seq[BundleData]): BundleData =
  result.importSource = "merged"
  result.importTime = now().utc.format("yyyy-MM-dd'T'HH:mm:ss'Z'")

  # Use the richest vehicle record (most fields populated)
  var bestScore = 0
  for b in bundles:
    var score = 0
    if b.vehicle.vin.len > 0: score += 10
    if b.vehicle.series.len > 0: score += 1
    if b.vehicle.engine.len > 0: score += 1
    if b.vehicle.modelYear.len > 0: score += 1
    if b.vehicle.iLevel.len > 0: score += 2
    if b.vehicle.mileageKm > 0: score += 1
    if score > bestScore:
      bestScore = score
      result.vehicle = b.vehicle

  # Merge ECUs by address, preferring entries with more data
  var ecuMap: Table[int, BundleEcu]
  for b in bundles:
    for e in b.ecus:
      if e.address notin ecuMap or e.hwVersion.len > 0:
        ecuMap[e.address] = e
  for _, e in ecuMap:
    result.ecus.add e
  result.ecus.sort(proc(a, b: BundleEcu): int = cmp(a.address, b.address))

  # Merge faults, dedup by (ecu, code)
  var seen: Table[string, bool]
  for b in bundles:
    for f in b.faults:
      let key = &"{f.ecu}:{f.code}"
      if key notin seen:
        seen[key] = true
        result.faults.add f

# ---------------------------------------------------------------------------
# Output: write bundle JSON files
# ---------------------------------------------------------------------------
proc vehicleToJson(v: BundleVehicle): JsonNode =
  %*{
    "vin": v.vin,
    "brand": v.brand,
    "series": v.series,
    "model_series": v.modelSeries,
    "body": v.body,
    "engine": v.engine,
    "transmission": v.transmission,
    "model_year": v.modelYear,
    "market": v.market,
    "i_level": v.iLevel,
    "comm_type": v.commType,
    "mileage_km": v.mileageKm,
    "data_source": v.source,
  }

proc faultToJson(f: BundleFault): JsonNode =
  %*{
    "ecu": f.ecu,
    "code": f.code,
    "description": f.description,
    "status": f.status,
    "p_code": f.pCode,
    "safety_relevant": f.safetyRelevant,
    "data_source": f.source,
  }

proc ecuToJson(e: BundleEcu): JsonNode =
  %*{
    "name": e.name,
    "full_name": e.fullName,
    "variant": e.variant,
    "address": e.address,
    "bus": e.bus,
    "comm_ok": e.commOk,
    "hw_version": e.hwVersion,
    "sw_version": e.swVersion,
    "supplier": e.supplier,
    "serial": e.serial,
    "data_source": e.source,
  }

proc writeBundle(data: BundleData, outDir: string) =
  createDir(outDir)

  let vehicleJson = vehicleToJson(data.vehicle)
  writeFile(outDir / "vehicle.json", pretty(vehicleJson))

  var faultsArr = newJArray()
  for f in data.faults: faultsArr.add faultToJson(f)
  writeFile(outDir / "faults.json", pretty(faultsArr))

  var ecusArr = newJArray()
  for e in data.ecus: ecusArr.add ecuToJson(e)
  writeFile(outDir / "ecus.json", pretty(ecusArr))

  let meta = %*{
    "import_source": data.importSource,
    "import_time": data.importTime,
    "fault_count": data.faults.len,
    "ecu_count": data.ecus.len,
    "sources": (block:
      var s: seq[string]
      for f in data.faults:
        if f.source notin s: s.add f.source
      for e in data.ecus:
        if e.source notin s: s.add e.source
      s),
  }
  writeFile(outDir / "import_meta.json", pretty(meta))

proc outputJsonStdout(data: BundleData) =
  let j = %*{
    "vehicle": vehicleToJson(data.vehicle),
    "faults": (block:
      var arr = newJArray()
      for f in data.faults: arr.add faultToJson(f)
      arr),
    "ecus": (block:
      var arr = newJArray()
      for e in data.ecus: arr.add ecuToJson(e)
      arr),
    "import_source": data.importSource,
    "import_time": data.importTime,
  }
  stdout.write(pretty(j))
  stdout.write("\n")

# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------
type
  ImportFormat = enum
    fmtBmweb, fmtBeemuu, fmtSvietlik, fmtKlartext, fmtAuto

  CliOptions = object
    files: seq[tuple[path: string, format: ImportFormat]]
    outDir: string
    jsonOutput: bool
    showHelp: bool

proc detectFormat(path: string): ImportFormat =
  let raw = readFile(path)
  let j = parseJson(raw)
  # Heuristic detection based on JSON structure
  if j.hasKey("snapshot"):
    return fmtBeemuu
  if j.hasKey("modules") and j.hasKey("battery_voltage"):
    return fmtSvietlik
  if j.hasKey("modules") and j.hasKey("scan_time"):
    return fmtBmweb
  if j.hasKey("vehicle") and j.hasKey("modules"):
    return fmtBmweb
  if j.hasKey("ecus") and j.hasKey("faults"):
    return fmtKlartext
  # Default guess
  return fmtBmweb

proc printUsage() =
  echo "ista-import: Multi-format BMW diagnostic data importer"
  echo ""
  echo "Imports scan data from BMWeb, Beemuu, svietlik, and klartext,"
  echo "normalizes it into the ista-bridge bundle format."
  echo ""
  echo "This tool is READ-ONLY — it transforms exported files, never"
  echo "contacts the car."
  echo ""
  echo "Usage:"
  echo "  ista-import --bmweb <file.json> [--out <dir>]"
  echo "  ista-import --beemuu <file.json> [--out <dir>]"
  echo "  ista-import --svietlik <file.json> [--out <dir>]"
  echo "  ista-import --klartext <file.json> [--out <dir>]"
  echo "  ista-import --auto <file.json>     (detect format)"
  echo "  ista-import --bmweb a.json --beemuu b.json  (merge)"
  echo ""
  echo "Options:"
  echo "  --bmweb <file>      Import BMWeb JSON scan"
  echo "  --beemuu <file>     Import Beemuu snapshot"
  echo "  --svietlik <file>   Import svietlik module scan"
  echo "  --klartext <file>   Import klartext diagnostic dump"
  echo "  --auto <file>       Auto-detect format from JSON structure"
  echo "  --out <dir>         Output directory (default: ./import_bundle)"
  echo "  --json, -j          Write merged result to stdout as JSON"
  echo "  --help, -h          Show this help"
  echo ""
  echo "Multiple files can be specified to merge data from different sources."
  echo "Each data point in the output is tagged with its source."

proc parseCli(): CliOptions =
  result.outDir = "import_bundle"

  var p = initOptParser(commandLineParams())
  while true:
    p.next()
    case p.kind
    of cmdEnd: break
    of cmdArgument:
      # Bare argument = auto-detect format
      result.files.add (path: p.key, format: fmtAuto)
    of cmdLongOption, cmdShortOption:
      case p.key.toLowerAscii
      of "bmweb":
        p.next()
        if p.kind == cmdArgument:
          result.files.add (path: p.key, format: fmtBmweb)
      of "beemuu":
        p.next()
        if p.kind == cmdArgument:
          result.files.add (path: p.key, format: fmtBeemuu)
      of "svietlik":
        p.next()
        if p.kind == cmdArgument:
          result.files.add (path: p.key, format: fmtSvietlik)
      of "klartext":
        p.next()
        if p.kind == cmdArgument:
          result.files.add (path: p.key, format: fmtKlartext)
      of "auto":
        p.next()
        if p.kind == cmdArgument:
          result.files.add (path: p.key, format: fmtAuto)
      of "out", "o": result.outDir = p.val
      of "json", "j": result.jsonOutput = true
      of "help", "h": result.showHelp = true
      else: discard

when isMainModule:
  let opts = parseCli()

  if opts.showHelp or opts.files.len == 0:
    printUsage()
    quit(0)

  var bundles: seq[BundleData]

  for entry in opts.files:
    if not fileExists(entry.path):
      stderr.writeLine &"Error: file not found: {entry.path}"
      quit(1)

    let fmt = if entry.format == fmtAuto: detectFormat(entry.path) else: entry.format

    let bundle = case fmt
      of fmtBmweb: importBmweb(entry.path)
      of fmtBeemuu: importBeemuu(entry.path)
      of fmtSvietlik: importSvietlik(entry.path)
      of fmtKlartext: importKlartext(entry.path)
      of fmtAuto: importBmweb(entry.path)  # fallback

    bundles.add bundle

    let fmtName = case fmt
      of fmtBmweb: "BMWeb"
      of fmtBeemuu: "Beemuu"
      of fmtSvietlik: "svietlik"
      of fmtKlartext: "klartext"
      of fmtAuto: "auto"
    stderr.writeLine &"Imported {entry.path} as {fmtName}: {bundle.ecus.len} ECUs, {bundle.faults.len} faults"

  let merged = if bundles.len == 1: bundles[0] else: merge(bundles)

  if opts.jsonOutput:
    outputJsonStdout(merged)
  else:
    writeBundle(merged, opts.outDir)
    stderr.writeLine &"Bundle written to {opts.outDir}/"
    stderr.writeLine &"  vehicle.json — {merged.vehicle.vin}"
    stderr.writeLine &"  faults.json  — {merged.faults.len} faults"
    stderr.writeLine &"  ecus.json    — {merged.ecus.len} ECUs"
    if merged.importSource == "merged":
      var sources: seq[string]
      for e in merged.ecus:
        if e.source notin sources: sources.add e.source
      for f in merged.faults:
        if f.source notin sources: sources.add f.source
      stderr.writeLine &"  Sources: {sources.join(\", \")}"
