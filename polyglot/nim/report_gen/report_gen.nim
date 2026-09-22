import std/[json, os, strutils, strformat, times, parseopt, algorithm, sequtils, base64]

type
  OutputFormat = enum
    ofMarkdown, ofText, ofHtml, ofSummary

  Options = object
    dataDir: string
    vehiclePath: string
    faultsPath: string
    ecusPath: string
    ccMessagesPath: string
    testsPath: string
    timelinePath: string
    ecukomPath: string
    screenshotsDir: string
    outputPath: string
    format: OutputFormat
    sessionDate: string
    sessionDuration: string

  Vehicle = object
    vin, brand, series, modelSeries, body, engine, transmission: string
    modelYear, market, steering, assemblyCountry: string
    iLevel, commType: string
    mileageKm: int
    typeKey, productionYear, productionMonth, gearboxType: string
    characteristics: seq[string]
    present: bool

  Fault = object
    ecu, ecuFullName, bus, description, status, warning, pCode: string
    code: int
    mileageKm: int
    saeCode, title, ecuVariant, ecuGroup: string
    weighting: int
    safetyRelevant: bool

  Ecu = object
    name, fullName, variant, bus, protocol, supplier: string
    faultCount: int
    commOk: bool
    softwareIds: seq[string]
    title, group: string
    address: int

  CcMessage = object
    ccId: int
    title, longText, cause: string

  FASTATest = object
    name, id, result, detail: string

  TimelineEvent = object
    timestamp, level, source, message: string

  ECUKomResult = object
    name, value, format: string

  ECUKomJob = object
    ecu, sgbd, jobName: string
    results: seq[ECUKomResult]

  ECUKomData = object
    jobs: seq[ECUKomJob]

proc getStr(node: JsonNode, key: string, default = ""): string =
  if node.hasKey(key) and node[key].kind == JString: node[key].getStr
  else: default

proc getIntField(node: JsonNode, key: string, default = 0): int =
  if node.hasKey(key):
    if node[key].kind == JInt: return node[key].getInt
    if node[key].kind == JFloat: return int(node[key].getFloat)
  default

proc getBoolField(node: JsonNode, key: string, default = false): bool =
  if node.hasKey(key) and node[key].kind == JBool: node[key].getBool
  else: default

proc tryLoadJson(path: string): JsonNode =
  if path.len == 0 or not fileExists(path):
    return nil
  try:
    result = parseFile(path)
  except JsonParsingError as e:
    stderr.writeLine(&"Warning: failed to parse '{path}': {e.msg}")
    result = nil
  except IOError as e:
    stderr.writeLine(&"Warning: could not read '{path}': {e.msg}")
    result = nil

proc loadVehicle(path: string): Vehicle =
  let node = tryLoadJson(path)
  if node.isNil or node.kind != JObject:
    return Vehicle(present: false)
  result = Vehicle(
    vin: node.getStr("vin"),
    brand: node.getStr("brand"),
    series: node.getStr("series"),
    modelSeries: node.getStr("model_series"),
    body: node.getStr("body"),
    engine: node.getStr("engine"),
    transmission: node.getStr("transmission"),
    modelYear: node.getStr("model_year"),
    market: node.getStr("market"),
    steering: node.getStr("steering"),
    assemblyCountry: node.getStr("assembly_country"),
    iLevel: node.getStr("i_level"),
    commType: node.getStr("comm_type"),
    mileageKm: node.getIntField("mileage_km"),
    typeKey: node.getStr("type_key"),
    productionYear: node.getStr("production_year"),
    productionMonth: node.getStr("production_month"),
    gearboxType: node.getStr("gearbox_type"),
    present: true,
  )
  if node.hasKey("characteristics") and node["characteristics"].kind == JArray:
    for item in node["characteristics"]:
      if item.kind == JString:
        result.characteristics.add(item.getStr)

proc loadFaults(path: string): seq[Fault] =
  let node = tryLoadJson(path)
  if node.isNil or node.kind != JArray: return @[]
  for item in node:
    if item.kind != JObject: continue
    result.add Fault(
      ecu: item.getStr("ecu"),
      ecuFullName: item.getStr("ecu_full_name"),
      bus: item.getStr("bus"),
      code: item.getIntField("code"),
      description: item.getStr("description"),
      status: item.getStr("status"),
      warning: item.getStr("warning"),
      pCode: item.getStr("p_code"),
      mileageKm: item.getIntField("mileage_km"),
      saeCode: item.getStr("sae_code"),
      title: item.getStr("title"),
      ecuVariant: item.getStr("ecu_variant"),
      ecuGroup: item.getStr("ecu_group"),
      weighting: item.getIntField("weighting"),
      safetyRelevant: item.getBoolField("safety_relevant"),
    )

proc loadEcus(path: string): seq[Ecu] =
  let node = tryLoadJson(path)
  if node.isNil or node.kind != JArray: return @[]
  for item in node:
    if item.kind != JObject: continue
    var ecu = Ecu(
      name: item.getStr("name"),
      fullName: item.getStr("full_name"),
      variant: item.getStr("variant"),
      bus: item.getStr("bus"),
      protocol: item.getStr("protocol"),
      supplier: item.getStr("supplier"),
      faultCount: item.getIntField("fault_count"),
      commOk: item.getBoolField("comm_ok"),
      title: item.getStr("title"),
      group: item.getStr("group"),
      address: item.getIntField("address"),
    )
    if item.hasKey("software_ids") and item["software_ids"].kind == JArray:
      for id in item["software_ids"]:
        if id.kind == JString:
          ecu.softwareIds.add(id.getStr)
    result.add ecu

proc loadCcMessages(path: string): seq[CcMessage] =
  let node = tryLoadJson(path)
  if node.isNil or node.kind != JArray: return @[]
  for item in node:
    if item.kind != JObject: continue
    result.add CcMessage(
      ccId: item.getIntField("cc_id"),
      title: item.getStr("title"),
      longText: item.getStr("long_text"),
      cause: item.getStr("cause"),
    )

proc loadTests(path: string): seq[FASTATest] =
  let node = tryLoadJson(path)
  if node.isNil or node.kind != JArray: return @[]
  for item in node:
    if item.kind != JObject: continue
    result.add FASTATest(
      name: item.getStr("name"),
      id: item.getStr("id"),
      result: item.getStr("result"),
      detail: item.getStr("detail"),
    )

proc loadTimeline(path: string): seq[TimelineEvent] =
  let node = tryLoadJson(path)
  if node.isNil or node.kind != JArray: return @[]
  for item in node:
    if item.kind != JObject: continue
    result.add TimelineEvent(
      timestamp: item.getStr("timestamp"),
      level: item.getStr("level"),
      source: item.getStr("source"),
      message: item.getStr("message"),
    )

proc loadECUKom(path: string): ECUKomData =
  let node = tryLoadJson(path)
  if node.isNil or node.kind != JObject: return
  if not node.hasKey("jobs") or node["jobs"].kind != JArray: return
  for job in node["jobs"]:
    if job.kind != JObject: continue
    var j = ECUKomJob(
      ecu: job.getStr("ecu"),
      sgbd: job.getStr("sgbd"),
      jobName: job.getStr("job_name"),
    )
    if job.hasKey("results") and job["results"].kind == JArray:
      for r in job["results"]:
        if r.kind != JObject: continue
        j.results.add ECUKomResult(
          name: r.getStr("name"),
          value: r.getStr("value"),
          format: r.getStr("format"),
        )
    result.jobs.add j

proc esc(s: string): string =
  result = s
  result = result.replace("&", "&amp;")
  result = result.replace("<", "&lt;")
  result = result.replace(">", "&gt;")
  result = result.replace("\"", "&quot;")

proc sortFaultsSafetyFirst(faults: seq[Fault]): seq[Fault] =
  result = faults
  proc cmp(a, b: Fault): int =
    if a.safetyRelevant != b.safetyRelevant:
      return (if a.safetyRelevant: -1 else: 1)
    if a.weighting != b.weighting:
      return b.weighting - a.weighting
    return 0
  result.sort(cmp)

proc faultDisplayCode(f: Fault): string =
  if f.pCode.len > 0: return f.pCode
  if f.saeCode.len > 0: return f.saeCode
  if f.code != 0: return "0x" & toHex(f.code, 4)
  return "-"

proc faultDisplayDesc(f: Fault): string =
  if f.description.len > 0: return f.description
  return f.title

proc faultDisplayEcu(f: Fault): string =
  if f.ecu.len > 0: return f.ecu
  if f.ecuVariant.len > 0: return f.ecuVariant
  return "-"

proc ecuDisplayName(e: Ecu): string =
  if e.name.len > 0: return e.name
  if e.title.len > 0: return e.title
  return "-"

# --- Markdown ---

proc renderMarkdown(vehicle: Vehicle, faults: seq[Fault], ecus: seq[Ecu],
                     ccMessages: seq[CcMessage], tests: seq[FASTATest],
                     timeline: seq[TimelineEvent], ecukom: ECUKomData): string =
  var s = newStringOfCap(8192)
  s.add "# ISTA Diagnostic Report\n\n"
  s.add &"**Generated:** {now().format(\"yyyy-MM-dd HH:mm:ss\")}\n\n"

  s.add "## Vehicle Information\n\n"
  if vehicle.present:
    s.add "| Field | Value |\n|---|---|\n"
    if vehicle.vin.len > 0: s.add &"| VIN | {vehicle.vin} |\n"
    if vehicle.brand.len > 0: s.add &"| Brand | {vehicle.brand} |\n"
    if vehicle.modelSeries.len > 0 or vehicle.series.len > 0:
      let model = if vehicle.modelSeries.len > 0: vehicle.modelSeries else: vehicle.series
      s.add &"| Model | {vehicle.modelYear} {model} ({vehicle.body}) |\n"
    if vehicle.engine.len > 0: s.add &"| Engine | {vehicle.engine} |\n"
    if vehicle.transmission.len > 0: s.add &"| Transmission | {vehicle.transmission} |\n"
    if vehicle.market.len > 0: s.add &"| Market | {vehicle.market} |\n"
    if vehicle.iLevel.len > 0: s.add &"| I-Level | {vehicle.iLevel} |\n"
    if vehicle.mileageKm > 0: s.add &"| Mileage | {vehicle.mileageKm} km |\n"
    if vehicle.commType.len > 0: s.add &"| Communication | {vehicle.commType} |\n"
    if vehicle.typeKey.len > 0: s.add &"| Type Key | {vehicle.typeKey} |\n"
    if vehicle.gearboxType.len > 0: s.add &"| Gearbox Type | {vehicle.gearboxType} |\n"
    let chars = if vehicle.characteristics.len > 0: vehicle.characteristics.join(", ") else: ""
    if chars.len > 0: s.add &"| Characteristics | {chars} |\n"
  else:
    s.add "_No vehicle data available._\n"
  s.add "\n"

  s.add "## Fault Codes\n\n"
  if faults.len > 0:
    s.add &"Found {faults.len} fault code(s):\n\n"
    s.add "| ECU | Bus | Code | Description | Status | Warning |\n"
    s.add "|-----|-----|------|-------------|--------|--------|\n"
    let sorted = sortFaultsSafetyFirst(faults)
    for f in sorted:
      var desc = f.faultDisplayDesc
      if desc.len > 60: desc = desc[0..56] & "..."
      s.add &"| {f.faultDisplayEcu} | {f.bus} | {f.faultDisplayCode} | {desc} | {f.status} | {f.warning} |\n"
    let safetyCount = sorted.filterIt(it.safetyRelevant).len
    if safetyCount > 0:
      s.add &"\n_{sorted.len} fault(s) total, {safetyCount} safety-relevant._\n"
  else:
    s.add "No fault codes found.\n"
  s.add "\n"

  s.add "## ECU List\n\n"
  if ecus.len > 0:
    s.add &"Found {ecus.len} ECU(s):\n\n"
    s.add "| ECU | Full Name | Bus | Protocol | Supplier | Faults |\n"
    s.add "|-----|-----------|-----|----------|----------|--------|\n"
    for e in ecus:
      let name = e.ecuDisplayName
      let fullName = if e.fullName.len > 0: e.fullName else: e.title
      s.add &"| {name} | {fullName} | {e.bus} | {e.protocol} | {e.supplier} | {e.faultCount} |\n"
  else:
    s.add "No ECU data available.\n"
  s.add "\n"

  if ecus.len > 0:
    var hasVersions = false
    for e in ecus:
      if e.softwareIds.len > 0: hasVersions = true; break
    if hasVersions:
      s.add "## Software Versions\n\n"
      if vehicle.iLevel.len > 0:
        s.add &"- **I-Level:** {vehicle.iLevel}\n"
      for e in ecus:
        if e.softwareIds.len == 0: continue
        s.add &"\n### {e.ecuDisplayName}\n"
        for id in e.softwareIds:
          s.add &"- {id}\n"

  if ccMessages.len > 0:
    s.add "\n## Check Control Messages\n\n"
    for m in ccMessages:
      s.add &"### CC {m.ccId}: {m.title}\n\n"
      if m.longText.len > 0: s.add &"{m.longText}\n\n"
      if m.cause.len > 0: s.add &"**Cause:** {m.cause}\n\n"

  if tests.len > 0:
    s.add "\n## FASTA Test Results\n\n"
    var passed, failed, skipped = 0
    for t in tests:
      case t.result
      of "passed": inc passed
      of "failed": inc failed
      of "skipped": inc skipped
      else: discard
    s.add &"**{passed}** passed, **{failed}** failed, **{skipped}** skipped\n\n"
    s.add "| Test | Result | Detail |\n|------|--------|--------|\n"
    for t in tests:
      let name = if t.name.len > 0: t.name else: t.id
      var detail = t.detail
      if detail.len > 50: detail = detail[0..46] & "..."
      s.add &"| {name} | {t.result} | {detail} |\n"

  if timeline.len > 0:
    s.add &"\n## Session Timeline\n\n"
    s.add &"{timeline.len} key events extracted from IstaOperation.log:\n\n"
    s.add "| Time | Level | Event |\n|------|-------|-------|\n"
    for e in timeline:
      s.add &"| {e.timestamp} | {e.level} | {e.message} |\n"

  s.add "\n---\n*Generated by ista-bridge*\n"
  result = s

# --- Text ---

proc renderText(vehicle: Vehicle, faults: seq[Fault], ecus: seq[Ecu],
                 ccMessages: seq[CcMessage], tests: seq[FASTATest],
                 timeline: seq[TimelineEvent]): string =
  var s = newStringOfCap(4096)
  let sep = repeat('=', 60)
  let subSep = repeat('-', 60)

  s.add "ISTA DIAGNOSTIC REPORT\n" & sep & "\n"
  s.add &"Generated: {now().format(\"yyyy-MM-dd HH:mm:ss\")}\n\n"

  s.add "VEHICLE SUMMARY\n" & subSep & "\n"
  if vehicle.present:
    if vehicle.vin.len > 0: s.add &"VIN:             {vehicle.vin}\n"
    if vehicle.brand.len > 0: s.add &"Brand:           {vehicle.brand}\n"
    if vehicle.modelSeries.len > 0: s.add &"Model:           {vehicle.modelYear} {vehicle.modelSeries} ({vehicle.body})\n"
    if vehicle.engine.len > 0: s.add &"Engine:          {vehicle.engine}\n"
    if vehicle.iLevel.len > 0: s.add &"I-Level:         {vehicle.iLevel}\n"
    if vehicle.mileageKm > 0: s.add &"Mileage:         {vehicle.mileageKm} km\n"
    if vehicle.commType.len > 0: s.add &"Communication:   {vehicle.commType}\n"
    if vehicle.typeKey.len > 0: s.add &"Type Key:        {vehicle.typeKey}\n"
    if vehicle.gearboxType.len > 0: s.add &"Gearbox Type:    {vehicle.gearboxType}\n"
    let chars = if vehicle.characteristics.len > 0: vehicle.characteristics.join(", ") else: ""
    if chars.len > 0: s.add &"Characteristics: {chars}\n"
  else:
    s.add "No vehicle data available.\n"
  s.add "\n"

  s.add "FAULT CODES\n" & subSep & "\n"
  if faults.len > 0:
    let sorted = sortFaultsSafetyFirst(faults)
    for f in sorted:
      let safetyMark = if f.safetyRelevant: " [SAFETY]" else: ""
      s.add &"[{f.faultDisplayCode}] {f.faultDisplayDesc}{safetyMark}\n"
      s.add &"    ECU: {f.faultDisplayEcu} ({f.bus})  Status: {f.status}\n"
    let safetyCount = sorted.filterIt(it.safetyRelevant).len
    s.add &"\n{sorted.len} fault(s) total, {safetyCount} safety-relevant.\n"
  else:
    s.add "No fault codes reported.\n"
  s.add "\n"

  s.add "ECU OVERVIEW\n" & subSep & "\n"
  if ecus.len > 0:
    for e in ecus:
      let name = e.ecuDisplayName
      s.add &"{name} - {e.fullName} [{e.bus}] {e.protocol}\n"
  else:
    s.add "No ECU data available.\n"
  s.add "\n"

  if ccMessages.len > 0:
    s.add "CHECK CONTROL MESSAGES\n" & subSep & "\n"
    for m in ccMessages:
      s.add &"CC {m.ccId}: {m.title}\n"
      if m.longText.len > 0: s.add &"  {m.longText}\n"
      if m.cause.len > 0: s.add &"  Cause: {m.cause}\n"
      s.add "\n"

  if tests.len > 0:
    s.add "FASTA TEST RESULTS\n" & subSep & "\n"
    for t in tests:
      let name = if t.name.len > 0: t.name else: t.id
      s.add &"  [{t.result}] {name}\n"
    s.add "\n"

  if timeline.len > 0:
    s.add "SESSION TIMELINE\n" & subSep & "\n"
    for e in timeline:
      s.add &"  {e.timestamp} [{e.level}] {e.message}\n"
    s.add "\n"

  result = s

# --- Summary (LLM-optimized) ---

proc renderSummary(vehicle: Vehicle, faults: seq[Fault], ecus: seq[Ecu],
                    tests: seq[FASTATest], timeline: seq[TimelineEvent],
                    ecukom: ECUKomData, sessionDate, sessionDuration: string): string =
  var s = newStringOfCap(4096)
  s.add "# ISTA Diagnostic Session\n\n"
  s.add "> Feed this file to Claude or ChatGPT for AI-assisted vehicle troubleshooting.\n"
  s.add "> The JSON files in this bundle have full structured data.\n\n"

  s.add "## Vehicle\n\n| Field | Value |\n|-------|-------|\n"
  if vehicle.vin.len > 0: s.add &"| VIN | {vehicle.vin} |\n"
  if vehicle.brand.len > 0: s.add &"| Brand | {vehicle.brand} |\n"
  if vehicle.modelSeries.len > 0:
    s.add &"| Model | {vehicle.modelYear} {vehicle.modelSeries} ({vehicle.body}) |\n"
  if vehicle.engine.len > 0: s.add &"| Engine | {vehicle.engine} |\n"
  if vehicle.transmission.len > 0: s.add &"| Transmission | {vehicle.transmission} |\n"
  if vehicle.market.len > 0: s.add &"| Market | {vehicle.market} |\n"
  if vehicle.iLevel.len > 0: s.add &"| I-Level | {vehicle.iLevel} |\n"
  if vehicle.mileageKm > 0: s.add &"| Mileage | {vehicle.mileageKm} km |\n"
  if vehicle.commType.len > 0: s.add &"| Connection | {vehicle.commType} |\n"

  s.add "\n## Session\n\n| Field | Value |\n|-------|-------|\n"
  if sessionDate.len > 0: s.add &"| Date | {sessionDate} |\n"
  if sessionDuration.len > 0: s.add &"| Duration | {sessionDuration} |\n"
  s.add &"| ECUs | {ecus.len} |\n"
  s.add &"| Faults | {faults.len} |\n"
  if tests.len > 0: s.add &"| FASTA Tests | {tests.len} |\n"
  if timeline.len > 0: s.add &"| Timeline Events | {timeline.len} |\n"
  if ecukom.jobs.len > 0: s.add &"| EDIABAS Jobs | {ecukom.jobs.len} |\n"

  s.add &"\n## Fault Codes ({faults.len} total)\n\n"
  if faults.len > 0:
    s.add "| ECU | Code | Description | Status |\n|-----|------|-------------|--------|\n"
    for f in faults:
      s.add &"| {f.faultDisplayEcu} | {f.faultDisplayCode} | {f.faultDisplayDesc} | {f.status} |\n"
  else:
    s.add "No fault codes stored.\n"

  if tests.len > 0:
    s.add &"\n## FASTA Test Results ({tests.len} total)\n\n"
    s.add "| Test | Result |\n|------|--------|\n"
    for t in tests:
      let name = if t.name.len > 0: t.name else: t.id
      s.add &"| {name} | {t.result} |\n"

  if timeline.len > 0:
    s.add "\n## Session Timeline (key events)\n\n"
    s.add "| Time | Level | Event |\n|------|-------|-------|\n"
    for e in timeline:
      s.add &"| {e.timestamp} | {e.level} | {e.message} |\n"

  s.add &"\n## ECU List ({ecus.len} total)\n\n"
  s.add "| Name | Full Name | Bus | Protocol | Supplier | Faults |\n"
  s.add "|------|-----------|-----|----------|----------|--------|\n"
  for e in ecus:
    s.add &"| {e.ecuDisplayName} | {e.fullName} | {e.bus} | {e.protocol} | {e.supplier} | {e.faultCount} |\n"

  result = s

# --- HTML ---

const htmlCSS = """
:root {
  --bg: #0d1117; --fg: #e6edf3; --muted: #8b949e; --border: #30363d;
  --surface: #161b22; --accent: #58a6ff; --green: #3fb950; --red: #f85149;
  --yellow: #d29922; --font: -apple-system, BlinkMacSystemFont, "Segoe UI", Helvetica, Arial, sans-serif;
  --mono: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
}
@media (prefers-color-scheme: light) {
  :root {
    --bg: #ffffff; --fg: #1f2328; --muted: #656d76; --border: #d0d7de;
    --surface: #f6f8fa; --accent: #0969da; --green: #1a7f37; --red: #cf222e;
    --yellow: #9a6700;
  }
}
*, *::before, *::after { box-sizing: border-box; }
body { font-family: var(--font); background: var(--bg); color: var(--fg); line-height: 1.6;
  max-width: 960px; margin: 0 auto; padding: 2rem 1.5rem; }
h1 { font-size: 1.75rem; border-bottom: 1px solid var(--border); padding-bottom: 0.5rem; margin-top: 0; }
h2 { font-size: 1.25rem; margin-top: 2.5rem; border-bottom: 1px solid var(--border); padding-bottom: 0.4rem; }
h3 { font-size: 1rem; margin-top: 1.5rem; }
a { color: var(--accent); text-decoration: none; }
code { font-family: var(--mono); font-size: 0.875em; background: var(--surface); padding: 0.2em 0.4em; border-radius: 4px; }
.meta { color: var(--muted); font-size: 0.875rem; margin-bottom: 1.5rem; }
.badge { display: inline-block; padding: 0.125rem 0.5rem; border-radius: 2rem; font-size: 0.75rem; font-weight: 600; }
.badge-pass { background: var(--green); color: #fff; }
.badge-fail { background: var(--red); color: #fff; }
.badge-skip { background: var(--yellow); color: #fff; }
.badge-info { background: var(--accent); color: #fff; }
.stats { display: flex; gap: 1rem; flex-wrap: wrap; margin: 1rem 0; }
.stat { background: var(--surface); border: 1px solid var(--border); border-radius: 8px; padding: 1rem 1.25rem; min-width: 120px; }
.stat-value { font-size: 1.5rem; font-weight: 700; }
.stat-label { font-size: 0.75rem; color: var(--muted); text-transform: uppercase; letter-spacing: 0.05em; }
table { width: 100%; border-collapse: collapse; margin: 1rem 0; font-size: 0.875rem; }
th { text-align: left; font-weight: 600; border-bottom: 2px solid var(--border); padding: 0.5rem; }
td { border-bottom: 1px solid var(--border); padding: 0.5rem; }
tr:hover td { background: var(--surface); }
.kv-table td:first-child { font-weight: 600; white-space: nowrap; width: 160px; color: var(--muted); }
.screenshots { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 0.75rem; margin: 1rem 0; }
.screenshots img { width: 100%; border-radius: 6px; border: 1px solid var(--border); cursor: pointer; transition: transform 0.15s; }
.screenshots img:hover { transform: scale(1.03); }
.timeline-row td:first-child { white-space: nowrap; font-family: var(--mono); font-size: 0.8rem; }
.level-error { color: var(--red); font-weight: 600; }
.level-warn { color: var(--yellow); }
footer { margin-top: 3rem; padding-top: 1rem; border-top: 1px solid var(--border); color: var(--muted); font-size: 0.75rem; }
@media print {
  body { max-width: none; padding: 0; color: #000; background: #fff; }
  .screenshots img { max-height: 150px; }
  tr:hover td { background: transparent; }
}"""

proc kvRow(key, value: string): string =
  if value.len == 0 or value == "0 km": return ""
  &"<tr><td>{esc(key)}</td><td>{esc(value)}</td></tr>\n"

proc statCard(value, label: string): string =
  &"""<div class="stat"><div class="stat-value">{esc(value)}</div><div class="stat-label">{esc(label)}</div></div>""" & "\n"

proc mimeForExt(ext: string): string =
  case ext.toLowerAscii
  of ".avif": "image/avif"
  of ".webp": "image/webp"
  of ".jpg", ".jpeg": "image/jpeg"
  else: "image/png"

proc renderHtml(vehicle: Vehicle, faults: seq[Fault], ecus: seq[Ecu],
                 tests: seq[FASTATest], timeline: seq[TimelineEvent],
                 ecukom: ECUKomData, screenshotsDir, sessionDate: string): string =
  var s = newStringOfCap(16384)
  let genTime = now().format("yyyy-MM-dd HH:mm:ss")
  let titleSuffix = if vehicle.vin.len > 0: " — " & esc(vehicle.vin) else: ""

  s.add "<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n"
  s.add "<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n"
  s.add &"<title>ISTA Diagnostic Report{titleSuffix}</title>\n"
  s.add "<style>\n" & htmlCSS & "\n</style>\n</head>\n<body>\n"

  s.add "<h1>ISTA Diagnostic Report</h1>\n"
  s.add &"""<p class="meta">Generated {esc(genTime)} &middot; Session {esc(sessionDate)}</p>""" & "\n"

  # Vehicle
  s.add "<h2>Vehicle</h2>\n"
  s.add "<table class=\"kv-table\">\n"
  s.add kvRow("VIN", vehicle.vin)
  s.add kvRow("Brand", vehicle.brand)
  if vehicle.modelSeries.len > 0:
    s.add kvRow("Model", &"{vehicle.modelYear} {vehicle.modelSeries} ({vehicle.body})")
  s.add kvRow("Engine", vehicle.engine)
  s.add kvRow("Transmission", vehicle.transmission)
  s.add kvRow("Market", vehicle.market)
  if vehicle.mileageKm > 0: s.add kvRow("Mileage", &"{vehicle.mileageKm} km")
  s.add kvRow("Communication", vehicle.commType)
  s.add kvRow("I-Level", vehicle.iLevel)
  s.add "</table>\n"

  # Stats
  s.add "<div class=\"stats\">\n"
  s.add statCard($ecus.len, "ECUs")
  s.add statCard($faults.len, "Faults")
  if tests.len > 0: s.add statCard($tests.len, "Tests")
  if ecukom.jobs.len > 0: s.add statCard($ecukom.jobs.len, "EDIABAS Jobs")
  s.add "</div>\n"

  # Faults
  s.add &"<h2>Fault Codes ({faults.len})</h2>\n"
  if faults.len == 0:
    s.add "<p>No fault codes found.</p>\n"
  else:
    s.add "<table>\n<tr><th>ECU</th><th>Bus</th><th>Code</th><th>Description</th><th>Status</th><th>Warning</th></tr>\n"
    for f in faults:
      s.add &"<tr><td>{esc(f.faultDisplayEcu)}</td><td>{esc(f.bus)}</td><td><code>{esc(f.faultDisplayCode)}</code></td>"
      s.add &"<td>{esc(f.faultDisplayDesc)}</td><td>{esc(f.status)}</td><td>{esc(f.warning)}</td></tr>\n"
    s.add "</table>\n"

  # FASTA
  if tests.len > 0:
    var passed, failed, skipped = 0
    for t in tests:
      case t.result
      of "passed": inc passed
      of "failed": inc failed
      of "skipped": inc skipped
      else: discard
    s.add &"<h2>FASTA Test Results ({tests.len})</h2>\n"
    s.add &"""<p><span class="badge badge-pass">{passed} passed</span> <span class="badge badge-fail">{failed} failed</span> <span class="badge badge-skip">{skipped} skipped</span></p>""" & "\n"
    s.add "<table>\n<tr><th>Test</th><th>Result</th><th>Detail</th></tr>\n"
    for t in tests:
      let name = if t.name.len > 0: t.name else: t.id
      let cls = case t.result
        of "passed": "badge-pass"
        of "failed": "badge-fail"
        of "skipped": "badge-skip"
        else: "badge-info"
      s.add &"""<tr><td>{esc(name)}</td><td><span class="badge {cls}">{esc(t.result)}</span></td><td>{esc(t.detail)}</td></tr>""" & "\n"
    s.add "</table>\n"

  # Timeline
  if timeline.len > 0:
    s.add &"<h2>Session Timeline ({timeline.len} events)</h2>\n"
    s.add "<table>\n<tr><th>Time</th><th>Level</th><th>Event</th></tr>\n"
    for e in timeline:
      let levelClass = case e.level
        of "ERROR": " class=\"level-error\""
        of "WARN": " class=\"level-warn\""
        else: ""
      s.add &"""<tr class="timeline-row"><td>{esc(e.timestamp)}</td><td{levelClass}>{esc(e.level)}</td><td>{esc(e.message)}</td></tr>""" & "\n"
    s.add "</table>\n"

  # ECUs
  if ecus.len > 0:
    s.add &"<h2>ECU List ({ecus.len})</h2>\n"
    s.add "<table>\n<tr><th>ECU</th><th>Full Name</th><th>Bus</th><th>Protocol</th><th>Supplier</th><th>Status</th></tr>\n"
    for e in ecus:
      let name = e.ecuDisplayName
      let fullName = if e.fullName.len > 0: e.fullName else: e.title
      let status = if e.commOk: "OK" elif e.faultCount > 0: &"{e.faultCount} faults" else: "—"
      s.add &"<tr><td>{esc(name)}</td><td>{esc(fullName)}</td><td>{esc(e.bus)}</td>"
      s.add &"<td>{esc(e.protocol)}</td><td>{esc(e.supplier)}</td><td>{esc(status)}</td></tr>\n"
    s.add "</table>\n"

  # Software versions
  var hasVersions = false
  for e in ecus:
    if e.softwareIds.len > 0: hasVersions = true; break
  if hasVersions:
    s.add "<h2>Software Versions</h2>\n"
    if vehicle.iLevel.len > 0:
      s.add &"<p><strong>I-Level:</strong> <code>{esc(vehicle.iLevel)}</code></p>\n"
    for e in ecus:
      if e.softwareIds.len == 0: continue
      s.add &"<h3>{esc(e.ecuDisplayName)}</h3>\n<ul>\n"
      for id in e.softwareIds:
        s.add &"<li><code>{esc(id)}</code></li>\n"
      s.add "</ul>\n"

  # Screenshots
  if screenshotsDir.len > 0 and dirExists(screenshotsDir):
    var imgs: seq[string]
    for entry in walkDir(screenshotsDir):
      if entry.kind != pcFile: continue
      let ext = splitFile(entry.path).ext.toLowerAscii
      if ext in [".avif", ".webp", ".png", ".jpg", ".jpeg"]:
        imgs.add entry.path
    imgs.sort()
    if imgs.len > 0:
      s.add &"<h2>Screenshots ({imgs.len})</h2>\n"
      s.add "<div class=\"screenshots\">\n"
      for imgPath in imgs:
        try:
          let data = readFile(imgPath)
          let encoded = encode(data)
          let mime = mimeForExt(splitFile(imgPath).ext)
          let fname = extractFilename(imgPath)
          s.add &"""<img src="data:{mime};base64,{encoded}" alt="{esc(fname)}" loading="lazy">""" & "\n"
        except IOError:
          discard
      s.add "</div>\n"

  s.add &"""<footer>Generated by <strong>ista-bridge</strong> &middot; {esc(genTime)}</footer>""" & "\n"
  s.add "</body>\n</html>\n"
  result = s

# --- CLI ---

proc printUsage() =
  echo """ista-report - ISTA diagnostic report generator

Usage:
  ista-report [options]

Options:
  --data-dir <dir>         Directory containing JSON data files
  --vehicle <file>         Path to vehicle.json
  --faults <file>          Path to faults.json
  --ecus <file>            Path to ecus.json
  --ccmessages <file>      Path to ccmessages.json
  --tests <file>           Path to tests.json (FASTA)
  --timeline <file>        Path to timeline.json
  --ecukom <file>          Path to ecukom.json
  --screenshots-dir <dir>  Directory with screenshots (HTML format)
  --output <file>          Write report to file (default: stdout)
  --format <fmt>           markdown, text, html, summary
  --session-date <date>    Session date for metadata
  --session-duration <dur> Session duration for summary
  --help, -h               Show this help message
"""

proc parseCliArgs(): Options =
  result = Options(format: ofMarkdown)
  var p = initOptParser(commandLineParams())

  proc takeValue(p: var OptParser, key: string): string =
    p.next()
    if p.kind == cmdArgument:
      result = p.key
    else:
      stderr.writeLine(&"Option --{key} requires a value")
      quit(1)

  p.next()
  while p.kind != cmdEnd:
    case p.kind
    of cmdArgument:
      discard
    of cmdLongOption, cmdShortOption:
      let key = p.key
      var val = p.val
      case key
      of "data-dir":
        if val.len == 0: val = takeValue(p, key)
        result.dataDir = val
      of "vehicle":
        if val.len == 0: val = takeValue(p, key)
        result.vehiclePath = val
      of "faults":
        if val.len == 0: val = takeValue(p, key)
        result.faultsPath = val
      of "ecus":
        if val.len == 0: val = takeValue(p, key)
        result.ecusPath = val
      of "ccmessages":
        if val.len == 0: val = takeValue(p, key)
        result.ccMessagesPath = val
      of "tests":
        if val.len == 0: val = takeValue(p, key)
        result.testsPath = val
      of "timeline":
        if val.len == 0: val = takeValue(p, key)
        result.timelinePath = val
      of "ecukom":
        if val.len == 0: val = takeValue(p, key)
        result.ecukomPath = val
      of "screenshots-dir":
        if val.len == 0: val = takeValue(p, key)
        result.screenshotsDir = val
      of "output", "o":
        if val.len == 0: val = takeValue(p, key)
        result.outputPath = val
      of "format", "f":
        if val.len == 0: val = takeValue(p, key)
        case val.toLowerAscii
        of "text", "txt": result.format = ofText
        of "markdown", "md", "": result.format = ofMarkdown
        of "html": result.format = ofHtml
        of "summary": result.format = ofSummary
        else:
          stderr.writeLine(&"Unknown format '{val}'")
          quit(1)
      of "session-date":
        if val.len == 0: val = takeValue(p, key)
        result.sessionDate = val
      of "session-duration":
        if val.len == 0: val = takeValue(p, key)
        result.sessionDuration = val
      of "help", "h":
        printUsage()
        quit(0)
      else:
        stderr.writeLine(&"Unknown option: --{key}")
        quit(1)
    of cmdEnd:
      discard
    p.next()

  if result.dataDir.len > 0:
    if result.vehiclePath.len == 0:
      result.vehiclePath = result.dataDir / "vehicle.json"
    if result.faultsPath.len == 0:
      result.faultsPath = result.dataDir / "faults.json"
    if result.ecusPath.len == 0:
      result.ecusPath = result.dataDir / "ecus.json"
    if result.ccMessagesPath.len == 0:
      result.ccMessagesPath = result.dataDir / "ccmessages.json"
    if result.testsPath.len == 0:
      result.testsPath = result.dataDir / "tests.json"
    if result.timelinePath.len == 0:
      result.timelinePath = result.dataDir / "timeline.json"
    if result.ecukomPath.len == 0:
      result.ecukomPath = result.dataDir / "ecukom.json"

proc main() =
  let opts = parseCliArgs()

  if opts.vehiclePath.len == 0 and opts.faultsPath.len == 0 and
     opts.ecusPath.len == 0 and opts.dataDir.len == 0:
    stderr.writeLine("No input files specified. Use --data-dir or individual file flags.")
    printUsage()
    quit(1)

  let vehicle = loadVehicle(opts.vehiclePath)
  let faults = loadFaults(opts.faultsPath)
  let ecus = loadEcus(opts.ecusPath)
  let ccMessages = loadCcMessages(opts.ccMessagesPath)
  let tests = loadTests(opts.testsPath)
  let timeline = loadTimeline(opts.timelinePath)
  let ecukom = loadECUKom(opts.ecukomPath)

  let report = case opts.format
    of ofMarkdown: renderMarkdown(vehicle, faults, ecus, ccMessages, tests, timeline, ecukom)
    of ofText: renderText(vehicle, faults, ecus, ccMessages, tests, timeline)
    of ofHtml: renderHtml(vehicle, faults, ecus, tests, timeline, ecukom, opts.screenshotsDir, opts.sessionDate)
    of ofSummary: renderSummary(vehicle, faults, ecus, tests, timeline, ecukom, opts.sessionDate, opts.sessionDuration)

  if opts.outputPath.len > 0:
    writeFile(opts.outputPath, report)
    stderr.writeLine(&"Report written to {opts.outputPath}")
  else:
    stdout.write(report)

when isMainModule:
  main()
