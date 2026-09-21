## ista-report - Diagnostic report generator
##
## Reads ISTA-style diagnostic JSON data files (vehicle info, fault codes,
## ECU listings, and check control messages) and renders a formatted
## diagnostic report in Markdown or plain text.
##
## Usage:
##   ista-report --data-dir ./data --output report.md
##   ista-report --data-dir ./data --format text
##   ista-report --vehicle vehicle.json --faults faults.json

import std/[json, os, strutils, strformat, times, parseopt, algorithm, sequtils]

type
  OutputFormat = enum
    ofMarkdown, ofText

  Options = object
    dataDir: string
    vehiclePath: string
    faultsPath: string
    ecusPath: string
    ccMessagesPath: string
    outputPath: string
    format: OutputFormat

  Vehicle = object
    vin, typeKey, productionYear, productionMonth, gearboxType: string
    characteristics: seq[string]
    present: bool

  Fault = object
    code, saeCode, title, ecuVariant, ecuGroup: string
    weighting: int
    safetyRelevant: bool

  Ecu = object
    name, title, group: string
    address: int

  CcMessage = object
    ccId: int
    title, longText, cause: string

# --------------------------------------------------------------------------
# JSON helpers
# --------------------------------------------------------------------------

proc getStr(node: JsonNode, key: string, default = ""): string =
  if node.hasKey(key) and node[key].kind == JString: node[key].getStr
  else: default

proc getIntField(node: JsonNode, key: string, default = 0): int =
  if node.hasKey(key) and node[key].kind == JInt: node[key].getInt
  else: default

proc getBoolField(node: JsonNode, key: string, default = false): bool =
  if node.hasKey(key) and node[key].kind == JBool: node[key].getBool
  else: default

proc tryLoadJson(path: string): JsonNode =
  ## Returns nil (JNull) if the file does not exist or fails to parse.
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

# --------------------------------------------------------------------------
# Loaders
# --------------------------------------------------------------------------

proc loadVehicle(path: string): Vehicle =
  let node = tryLoadJson(path)
  if node.isNil or node.kind != JObject:
    return Vehicle(present: false)

  result = Vehicle(
    vin: node.getStr("vin"),
    typeKey: node.getStr("type_key"),
    productionYear: node.getStr("production_year"),
    productionMonth: node.getStr("production_month"),
    gearboxType: node.getStr("gearbox_type"),
    present: true
  )
  if node.hasKey("characteristics") and node["characteristics"].kind == JArray:
    for item in node["characteristics"]:
      if item.kind == JString:
        result.characteristics.add(item.getStr)

proc loadFaults(path: string): seq[Fault] =
  let node = tryLoadJson(path)
  if node.isNil or node.kind != JArray:
    return @[]

  for item in node:
    if item.kind != JObject: continue
    result.add Fault(
      code: item.getStr("code"),
      saeCode: item.getStr("sae_code"),
      title: item.getStr("title"),
      ecuVariant: item.getStr("ecu_variant"),
      ecuGroup: item.getStr("ecu_group"),
      weighting: item.getIntField("weighting"),
      safetyRelevant: item.getBoolField("safety_relevant")
    )

proc loadEcus(path: string): seq[Ecu] =
  let node = tryLoadJson(path)
  if node.isNil or node.kind != JArray:
    return @[]

  for item in node:
    if item.kind != JObject: continue
    result.add Ecu(
      name: item.getStr("name"),
      title: item.getStr("title"),
      group: item.getStr("group"),
      address: item.getIntField("address")
    )

proc loadCcMessages(path: string): seq[CcMessage] =
  let node = tryLoadJson(path)
  if node.isNil or node.kind != JArray:
    return @[]

  for item in node:
    if item.kind != JObject: continue
    result.add CcMessage(
      ccId: item.getIntField("cc_id"),
      title: item.getStr("title"),
      longText: item.getStr("long_text"),
      cause: item.getStr("cause")
    )

# --------------------------------------------------------------------------
# Rendering
# --------------------------------------------------------------------------

proc sortFaultsSafetyFirst(faults: seq[Fault]): seq[Fault] =
  ## Stable sort: safety-relevant faults first, then by descending weighting.
  result = faults
  proc cmp(a, b: Fault): int =
    if a.safetyRelevant != b.safetyRelevant:
      return (if a.safetyRelevant: -1 else: 1)
    if a.weighting != b.weighting:
      return b.weighting - a.weighting
    return 0
  result.sort(cmp)

proc renderMarkdown(opts: Options, vehicle: Vehicle, faults: seq[Fault],
                     ecus: seq[Ecu], ccMessages: seq[CcMessage]): string =
  var s = newStringOfCap(4096)

  s.add "# ISTA Diagnostic Report\n\n"
  s.add &"_Generated: {now().format(\"yyyy-MM-dd HH:mm:ss\")}_\n\n"

  # Vehicle Summary
  s.add "## Vehicle Summary\n\n"
  if vehicle.present:
    s.add &"| Field | Value |\n"
    s.add &"|---|---|\n"
    s.add &"| VIN | {vehicle.vin} |\n"
    s.add &"| Type Key | {vehicle.typeKey} |\n"
    s.add &"| Production | {vehicle.productionMonth}/{vehicle.productionYear} |\n"
    s.add &"| Gearbox Type | {vehicle.gearboxType} |\n"
    let chars = if vehicle.characteristics.len > 0: vehicle.characteristics.join(", ") else: "-"
    s.add &"| Characteristics | {chars} |\n"
  else:
    s.add "_No vehicle data available._\n"
  s.add "\n"

  # Fault Codes
  s.add "## Fault Codes\n\n"
  if faults.len > 0:
    let sorted = sortFaultsSafetyFirst(faults)
    s.add "| Code | SAE Code | Title | ECU | Weighting | Safety |\n"
    s.add "|---|---|---|---|---|---|\n"
    for f in sorted:
      let safetyMark = if f.safetyRelevant: "**YES**" else: "no"
      s.add &"| {f.code} | {f.saeCode} | {f.title} | {f.ecuVariant} ({f.ecuGroup}) | {f.weighting} | {safetyMark} |\n"

    let safetyCount = sorted.filterIt(it.safetyRelevant).len
    s.add &"\n_{sorted.len} fault(s) total, {safetyCount} safety-relevant._\n"
  else:
    s.add "_No fault codes reported._\n"
  s.add "\n"

  # ECU Overview
  s.add "## ECU Overview\n\n"
  if ecus.len > 0:
    s.add "| Name | Title | Group | Address |\n"
    s.add "|---|---|---|---|\n"
    for e in ecus:
      let addrStr = "0x" & toHex(e.address, 2)
      s.add &"| {e.name} | {e.title} | {e.group} | {addrStr} |\n"
  else:
    s.add "_No ECU data available._\n"
  s.add "\n"

  # Check Control Messages
  s.add "## Check Control Messages\n\n"
  if ccMessages.len > 0:
    for m in ccMessages:
      s.add &"### CC {m.ccId}: {m.title}\n\n"
      if m.longText.len > 0:
        s.add &"{m.longText}\n\n"
      if m.cause.len > 0:
        s.add &"**Cause:** {m.cause}\n\n"
  else:
    s.add "_No check control messages available._\n"
  s.add "\n"

  result = s

proc renderText(opts: Options, vehicle: Vehicle, faults: seq[Fault],
                 ecus: seq[Ecu], ccMessages: seq[CcMessage]): string =
  var s = newStringOfCap(4096)
  let sep = repeat('=', 60)
  let subSep = repeat('-', 60)

  s.add "ISTA DIAGNOSTIC REPORT\n"
  s.add sep & "\n"
  s.add &"Generated: {now().format(\"yyyy-MM-dd HH:mm:ss\")}\n\n"

  s.add "VEHICLE SUMMARY\n" & subSep & "\n"
  if vehicle.present:
    s.add &"VIN:             {vehicle.vin}\n"
    s.add &"Type Key:        {vehicle.typeKey}\n"
    s.add &"Production:      {vehicle.productionMonth}/{vehicle.productionYear}\n"
    s.add &"Gearbox Type:    {vehicle.gearboxType}\n"
    let chars = if vehicle.characteristics.len > 0: vehicle.characteristics.join(", ") else: "-"
    s.add &"Characteristics: {chars}\n"
  else:
    s.add "No vehicle data available.\n"
  s.add "\n"

  s.add "FAULT CODES\n" & subSep & "\n"
  if faults.len > 0:
    let sorted = sortFaultsSafetyFirst(faults)
    for f in sorted:
      let safetyMark = if f.safetyRelevant: "[SAFETY]" else: ""
      s.add &"[{f.code}] {f.saeCode} - {f.title} {safetyMark}\n"
      s.add &"    ECU: {f.ecuVariant} ({f.ecuGroup})  Weighting: {f.weighting}\n"
    let safetyCount = sorted.filterIt(it.safetyRelevant).len
    s.add &"\n{sorted.len} fault(s) total, {safetyCount} safety-relevant.\n"
  else:
    s.add "No fault codes reported.\n"
  s.add "\n"

  s.add "ECU OVERVIEW\n" & subSep & "\n"
  if ecus.len > 0:
    for e in ecus:
      let addrStr = "0x" & toHex(e.address, 2)
      s.add &"{e.name} ({addrStr}) - {e.title} [{e.group}]\n"
  else:
    s.add "No ECU data available.\n"
  s.add "\n"

  s.add "CHECK CONTROL MESSAGES\n" & subSep & "\n"
  if ccMessages.len > 0:
    for m in ccMessages:
      s.add &"CC {m.ccId}: {m.title}\n"
      if m.longText.len > 0:
        s.add &"  {m.longText}\n"
      if m.cause.len > 0:
        s.add &"  Cause: {m.cause}\n"
      s.add "\n"
  else:
    s.add "No check control messages available.\n"
  s.add "\n"

  result = s

# --------------------------------------------------------------------------
# CLI
# --------------------------------------------------------------------------

proc printUsage() =
  echo """ista-report - ISTA diagnostic report generator

Usage:
  ista-report [options]

Options:
  --data-dir <dir>     Directory containing vehicle.json, faults.json,
                        ecus.json, ccmessages.json
  --vehicle <file>     Path to vehicle JSON file
  --faults <file>      Path to faults JSON file
  --ecus <file>        Path to ECUs JSON file
  --ccmessages <file>  Path to check control messages JSON file
  --output <file>      Write report to file (default: stdout)
  --format <fmt>       Output format: markdown (default) or text
  --help, -h           Show this help message

Examples:
  ista-report --data-dir ./data --output report.md
  ista-report --data-dir ./data --format text
  ista-report --vehicle vehicle.json --faults faults.json
"""

proc parseCliArgs(): Options =
  ## Manual next()-based parsing instead of a `for kind, key, val in p.getopt()`
  ## loop: Nim's parseopt only auto-attaches a value to a long/short option
  ## when written as `--opt:value` or `--opt=value`. The space-separated
  ## style used by this CLI (`--opt value`) arrives as two separate tokens
  ## (an option with an empty val, followed by a cmdArgument holding the
  ## value), so we look ahead and consume the next argument ourselves.
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
      of "output", "o":
        if val.len == 0: val = takeValue(p, key)
        result.outputPath = val
      of "format", "f":
        if val.len == 0: val = takeValue(p, key)
        case val.toLowerAscii
        of "text", "txt":
          result.format = ofText
        of "markdown", "md", "":
          result.format = ofMarkdown
        else:
          stderr.writeLine(&"Unknown format '{val}', expected 'markdown' or 'text'")
          quit(1)
      of "help", "h":
        printUsage()
        quit(0)
      else:
        stderr.writeLine(&"Unknown option: --{key}")
        quit(1)
    of cmdEnd:
      discard
    p.next()

  # Resolve default file locations from --data-dir when not set explicitly.
  if result.dataDir.len > 0:
    if result.vehiclePath.len == 0:
      result.vehiclePath = result.dataDir / "vehicle.json"
    if result.faultsPath.len == 0:
      result.faultsPath = result.dataDir / "faults.json"
    if result.ecusPath.len == 0:
      result.ecusPath = result.dataDir / "ecus.json"
    if result.ccMessagesPath.len == 0:
      result.ccMessagesPath = result.dataDir / "ccmessages.json"

proc main() =
  let opts = parseCliArgs()

  if opts.vehiclePath.len == 0 and opts.faultsPath.len == 0 and
     opts.ecusPath.len == 0 and opts.ccMessagesPath.len == 0:
    stderr.writeLine("No input files specified. Use --data-dir or individual --vehicle/--faults/--ecus/--ccmessages flags.")
    printUsage()
    quit(1)

  let vehicle = loadVehicle(opts.vehiclePath)
  let faults = loadFaults(opts.faultsPath)
  let ecus = loadEcus(opts.ecusPath)
  let ccMessages = loadCcMessages(opts.ccMessagesPath)

  let report =
    case opts.format
    of ofMarkdown: renderMarkdown(opts, vehicle, faults, ecus, ccMessages)
    of ofText: renderText(opts, vehicle, faults, ecus, ccMessages)

  if opts.outputPath.len > 0:
    writeFile(opts.outputPath, report)
    echo &"Report written to {opts.outputPath}"
  else:
    stdout.write(report)

when isMainModule:
  main()
