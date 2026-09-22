import std/[net, json, os, strutils, strformat, parseopt, times, endians, osproc]

# ista-enet: Read-only BMW ENET/HSFZ diagnostic client.
#
# SAFETY INVARIANTS — every one is load-bearing:
#   1. NEVER write to car systems. Write UDS services are hard-blocked at
#      the protocol layer — they raise SafetyError before serialization.
#   2. NEVER interfere with a running ISTA session. Before connecting, we
#      check for ISTA processes (ISTAGUI.exe, IstaServicesHost.exe). If
#      found, we refuse to connect — ISTA owns the diagnostic session.
#   3. ALWAYS clean up. On disconnect, we return every ECU we touched back
#      to the default diagnostic session (0x10 0x01).
#   4. Use tester address 0xF5 by default (ISTA uses 0xF4) so even if both
#      run simultaneously, UDS response routing stays separate.
#
# Based on the protocol approaches from klartext (Rust) and svietlik
# (TypeScript), rewritten in Nim for the ista-bridge satellite toolchain.

const
  DefaultPort = 6801
  DefaultHost = "169.254.0.10"
  DefaultTesterAddr = 0xF5'u16  # 0xF5 — deliberately different from ISTA's 0xF4
  HsfzHeaderLen = 6
  ConnectTimeoutMs = 5000
  ReadTimeoutMs = 3000

# ---------------------------------------------------------------------------
# ISTA session detection — refuse to connect if ISTA is running
# ---------------------------------------------------------------------------
# ISTA owns the diagnostic session. If ISTAGUI.exe or IstaServicesHost.exe
# is running, we must not connect — our HSFZ handshake, extended session
# requests, and tester-present keepalives could corrupt ISTA's active
# diagnostic communication with the vehicle.

proc isIstaRunning*(): bool =
  when defined(windows):
    try:
      let output = execProcess("tasklist", args = ["/FI", "IMAGENAME eq ISTAGUI.exe", "/NH"], options = {poUsePath})
      if "ISTAGUI.exe" in output:
        return true
      let output2 = execProcess("tasklist", args = ["/FI", "IMAGENAME eq IstaServicesHost.exe", "/NH"], options = {poUsePath})
      if "IstaServicesHost.exe" in output2:
        return true
    except CatchableError:
      discard
  else:
    try:
      let output = execProcess("pgrep", args = ["-f", "ISTAGUI|IstaServicesHost"], options = {poUsePath})
      if output.strip.len > 0:
        return true
    except CatchableError:
      discard
  return false

type IstaRunningError* = object of CatchableError

proc enforceNoIstaConflict*(force: bool = false) =
  if isIstaRunning():
    if force:
      stderr.writeLine "WARNING: ISTA is running! Using --force to override."
      stderr.writeLine "         This may interfere with ISTA's active diagnostic session."
      stderr.writeLine "         Close ISTA first if possible."
    else:
      raise newException(IstaRunningError,
        "ISTA is running (ISTAGUI.exe or IstaServicesHost.exe detected). " &
        "Connecting now could interfere with ISTA's active diagnostic session. " &
        "Close ISTA first, or use --force to override (not recommended).")

# ---------------------------------------------------------------------------
# HSFZ frame types
# ---------------------------------------------------------------------------
type
  HsfzType* = enum
    htDiagRequest     = 0x0001
    htDiagResponse    = 0x0002
    htAliveCheck      = 0x0012
    htAliveResponse   = 0x0013
    htErrorAck        = 0x00FF
    htHandshake       = 0x0040
    htHandshakeAck    = 0x0041

  HsfzFrame* = object
    typ*: uint16
    payload*: seq[byte]

# ---------------------------------------------------------------------------
# UDS services — exhaustive read-only allowlist
# ---------------------------------------------------------------------------
type
  UdsService* = enum
    usEcuReset                      = 0x11
    usDiagnosticSessionControl      = 0x10
    usTesterPresent                 = 0x3E
    usReadDataByIdentifier          = 0x22
    usReadDTCInformation            = 0x19
    usReadMemoryByAddress           = 0x23
    usSecurityAccess                = 0x27
    usCommunicationControl          = 0x28
    usControlDTCSetting             = 0x85
    usResponseOnEvent               = 0x86
    usReadDataByPeriodicIdentifier  = 0x2A

const ReadOnlyServices*: set[uint8] = {
  0x10'u8,  # DiagnosticSessionControl
  0x11'u8,  # EcuReset (read-only: used to exit extended session)
  0x19'u8,  # ReadDTCInformation
  0x22'u8,  # ReadDataByIdentifier
  0x23'u8,  # ReadMemoryByAddress
  0x27'u8,  # SecurityAccess (needed to unlock read-only extended data)
  0x2A'u8,  # ReadDataByPeriodicIdentifier
  0x3E'u8,  # TesterPresent
}

const BlockedServices*: set[uint8] = {
  0x14'u8,  # ClearDiagnosticInformation — clears DTCs
  0x2E'u8,  # WriteDataByIdentifier — writes parameters
  0x2F'u8,  # InputOutputControlByIdentifier — actuator control
  0x31'u8,  # RoutineControl — runs ECU routines
  0x34'u8,  # RequestDownload — flash/programming
  0x35'u8,  # RequestUpload — flash/programming
  0x36'u8,  # TransferData — flash/programming
  0x37'u8,  # RequestTransferExit — flash/programming
  0x3D'u8,  # WriteMemoryByAddress — writes memory
  0x28'u8,  # CommunicationControl — can disable comms
  0x85'u8,  # ControlDTCSetting — can suppress DTCs
}

# ---------------------------------------------------------------------------
# Safety gate: reject any UDS service not on the read-only allowlist
# ---------------------------------------------------------------------------
type SafetyError* = object of CatchableError

proc enforceReadOnly*(serviceId: uint8) =
  if serviceId in BlockedServices:
    raise newException(SafetyError,
      &"BLOCKED: UDS service 0x{serviceId:02X} is a write/mutate operation — read-only mode active")
  if serviceId notin ReadOnlyServices:
    raise newException(SafetyError,
      &"BLOCKED: UDS service 0x{serviceId:02X} is not in the read-only allowlist")

# ---------------------------------------------------------------------------
# HSFZ frame encode / decode
# ---------------------------------------------------------------------------
proc encodeFrame*(typ: uint16, payload: openArray[byte]): seq[byte] =
  let payloadLen = payload.len.uint32
  result = newSeq[byte](HsfzHeaderLen + payload.len)
  # Length: 4 bytes big-endian (payload length, NOT including header)
  result[0] = byte((payloadLen shr 24) and 0xFF)
  result[1] = byte((payloadLen shr 16) and 0xFF)
  result[2] = byte((payloadLen shr 8) and 0xFF)
  result[3] = byte(payloadLen and 0xFF)
  # Type: 2 bytes big-endian
  result[4] = byte((typ shr 8) and 0xFF)
  result[5] = byte(typ and 0xFF)
  # Payload
  if payload.len > 0:
    copyMem(addr result[HsfzHeaderLen], unsafeAddr payload[0], payload.len)

proc decodeFrameHeader*(data: openArray[byte]): tuple[payloadLen: uint32, typ: uint16] =
  assert data.len >= HsfzHeaderLen
  let payloadLen = (data[0].uint32 shl 24) or
                   (data[1].uint32 shl 16) or
                   (data[2].uint32 shl 8) or
                   data[3].uint32
  let typ = (data[4].uint16 shl 8) or data[5].uint16
  (payloadLen, typ)

# ---------------------------------------------------------------------------
# UDS request builders (read-only)
# ---------------------------------------------------------------------------
proc buildUdsRequest*(srcAddr, dstAddr: uint16, serviceId: uint8,
                      subData: openArray[byte] = []): seq[byte] =
  enforceReadOnly(serviceId)
  # HSFZ diagnostic payload: src(2) + dst(2) + UDS data
  var payload = newSeq[byte](4 + 1 + subData.len)
  payload[0] = byte((srcAddr shr 8) and 0xFF)
  payload[1] = byte(srcAddr and 0xFF)
  payload[2] = byte((dstAddr shr 8) and 0xFF)
  payload[3] = byte(dstAddr and 0xFF)
  payload[4] = serviceId
  if subData.len > 0:
    copyMem(addr payload[5], unsafeAddr subData[0], subData.len)
  encodeFrame(htDiagRequest.uint16, payload)

proc readDtcRequest*(srcAddr, dstAddr: uint16, subFunc: uint8 = 0x01): seq[byte] =
  # 0x19 ReadDTCInformation, subfunction 0x01 = reportNumberOfDTCByStatusMask
  # subfunction 0x02 = reportDTCByStatusMask
  buildUdsRequest(srcAddr, dstAddr, 0x19, [subFunc, 0xFF'u8])

proc readDataByIdRequest*(srcAddr, dstAddr: uint16, did: uint16): seq[byte] =
  # 0x22 ReadDataByIdentifier
  let didBytes = [byte((did shr 8) and 0xFF), byte(did and 0xFF)]
  buildUdsRequest(srcAddr, dstAddr, 0x22, didBytes)

proc testerPresentRequest*(srcAddr, dstAddr: uint16): seq[byte] =
  buildUdsRequest(srcAddr, dstAddr, 0x3E, [0x00'u8])

proc extendedSessionRequest*(srcAddr, dstAddr: uint16): seq[byte] =
  # 0x10 DiagnosticSessionControl, 0x03 = extendedDiagnosticSession
  buildUdsRequest(srcAddr, dstAddr, 0x10, [0x03'u8])

proc defaultSessionRequest*(srcAddr, dstAddr: uint16): seq[byte] =
  # 0x10 DiagnosticSessionControl, 0x01 = defaultSession
  buildUdsRequest(srcAddr, dstAddr, 0x10, [0x01'u8])

# ---------------------------------------------------------------------------
# HSFZ handshake
# ---------------------------------------------------------------------------
proc handshakePayload*(testerAddr: uint16): seq[byte] =
  # Handshake payload: 2-byte tester logical address
  @[byte((testerAddr shr 8) and 0xFF), byte(testerAddr and 0xFF)]

# ---------------------------------------------------------------------------
# UDS response parsing
# ---------------------------------------------------------------------------
type
  UdsResponse* = object
    srcAddr*: uint16
    dstAddr*: uint16
    serviceId*: uint8
    data*: seq[byte]
    isPositive*: bool
    nrc*: uint8  # Negative Response Code (if negative)

  DtcEntry* = object
    code*: uint32  # 3-byte DTC
    status*: uint8

  ReadDtcResult* = object
    statusAvailabilityMask*: uint8
    formatIdentifier*: uint8
    dtcCount*: uint16
    dtcs*: seq[DtcEntry]

proc parseUdsResponse*(payload: openArray[byte]): UdsResponse =
  assert payload.len >= 5  # src(2) + dst(2) + at least 1 byte UDS
  result.srcAddr = (payload[0].uint16 shl 8) or payload[1].uint16
  result.dstAddr = (payload[2].uint16 shl 8) or payload[3].uint16
  result.serviceId = payload[4]
  if payload.len > 5:
    result.data = @(payload[5 .. ^1])
  if result.serviceId == 0x7F:
    result.isPositive = false
    if result.data.len >= 2:
      result.nrc = result.data[1]
  else:
    result.isPositive = true

proc parseDtcResponse*(resp: UdsResponse): ReadDtcResult =
  # Positive response to 0x19 starts with 0x59
  if resp.serviceId != 0x59 or resp.data.len < 3:
    return
  let subFunc = resp.data[0]
  case subFunc
  of 0x01:
    # reportNumberOfDTCByStatusMask
    result.statusAvailabilityMask = resp.data[1]
    result.formatIdentifier = resp.data[2]
    if resp.data.len >= 5:
      result.dtcCount = (resp.data[3].uint16 shl 8) or resp.data[4].uint16
  of 0x02:
    # reportDTCByStatusMask
    result.statusAvailabilityMask = resp.data[1]
    var i = 2
    while i + 3 < resp.data.len:
      let dtcHigh = resp.data[i].uint32
      let dtcMid = resp.data[i + 1].uint32
      let dtcLow = resp.data[i + 2].uint32
      let status = resp.data[i + 3]
      result.dtcs.add DtcEntry(
        code: (dtcHigh shl 16) or (dtcMid shl 8) or dtcLow,
        status: status
      )
      i += 4
    result.dtcCount = result.dtcs.len.uint16
  else:
    discard

# ---------------------------------------------------------------------------
# Common DIDs (Data Identifiers) for BMW F-series
# ---------------------------------------------------------------------------
const
  DidVin*            = 0xF190'u16
  DidEcuSerial*      = 0xF18C'u16
  DidEcuHwVersion*   = 0xF191'u16
  DidEcuSwVersion*   = 0xF195'u16
  DidSupplier*       = 0xF18A'u16
  DidManufDate*      = 0xF18B'u16
  DidActiveSession*  = 0xF186'u16
  DidBatteryVoltage* = 0xF1F0'u16

# ---------------------------------------------------------------------------
# Network connection (TCP)
# ---------------------------------------------------------------------------
type
  EnetConnection* = ref object
    sock: Socket
    host: string
    port: int
    testerAddr: uint16
    connected: bool
    touchedEcus: seq[uint16]  # ECUs we switched to extended session — must clean up

proc newEnetConnection*(host: string = DefaultHost, port: int = DefaultPort,
                        testerAddr: uint16 = DefaultTesterAddr): EnetConnection =
  EnetConnection(
    sock: newSocket(),
    host: host,
    port: port,
    testerAddr: testerAddr,
    connected: false,
    touchedEcus: @[],
  )

proc connect*(conn: EnetConnection, force: bool = false) =
  enforceNoIstaConflict(force)
  conn.sock.connect(conn.host, Port(conn.port), ConnectTimeoutMs)
  conn.connected = true

proc cleanupSessions(conn: EnetConnection)

proc disconnect*(conn: EnetConnection) =
  if conn.connected:
    conn.cleanupSessions()
    conn.sock.close()
    conn.connected = false

proc sendFrame*(conn: EnetConnection, frame: openArray[byte]) =
  let sent = conn.sock.send(unsafeAddr frame[0], frame.len)
  if sent != frame.len:
    raise newException(IOError, "incomplete HSFZ frame send")

proc recvFrame*(conn: EnetConnection): HsfzFrame =
  var header: array[HsfzHeaderLen, byte]
  let hRead = conn.sock.recv(cast[pointer](addr header[0]), HsfzHeaderLen, ReadTimeoutMs)
  if hRead != HsfzHeaderLen:
    raise newException(IOError, &"HSFZ header read failed (got {hRead} bytes)")
  let (payloadLen, typ) = decodeFrameHeader(header)
  result.typ = typ
  if payloadLen > 0:
    result.payload = newSeq[byte](payloadLen)
    var totalRead = 0
    while totalRead < payloadLen.int:
      let n = conn.sock.recv(cast[pointer](addr result.payload[totalRead]),
                              payloadLen.int - totalRead, ReadTimeoutMs)
      if n <= 0:
        raise newException(IOError, "HSFZ payload read failed")
      totalRead += n

proc doHandshake*(conn: EnetConnection) =
  let payload = handshakePayload(conn.testerAddr)
  let frame = encodeFrame(htHandshake.uint16, payload)
  conn.sendFrame(frame)
  let resp = conn.recvFrame()
  if resp.typ != htHandshakeAck.uint16:
    raise newException(IOError, &"HSFZ handshake failed (got type 0x{resp.typ:04X})")

proc sendUds*(conn: EnetConnection, dstAddr: uint16, serviceId: uint8,
              subData: openArray[byte] = []): UdsResponse =
  enforceReadOnly(serviceId)
  # Track ECUs we switch to extended session so we can clean up on disconnect
  if serviceId == 0x10 and subData.len > 0 and subData[0] == 0x03:
    if dstAddr notin conn.touchedEcus:
      conn.touchedEcus.add dstAddr
  let frame = buildUdsRequest(conn.testerAddr, dstAddr, serviceId, subData)
  conn.sendFrame(frame)
  while true:
    let resp = conn.recvFrame()
    case resp.typ
    of htDiagResponse.uint16:
      return parseUdsResponse(resp.payload)
    of htAliveCheck.uint16:
      let ack = encodeFrame(htAliveResponse.uint16, [])
      conn.sendFrame(ack)
    of htErrorAck.uint16:
      raise newException(IOError, "HSFZ error acknowledgment received")
    else:
      discard

proc cleanupSessions(conn: EnetConnection) =
  for ecuAddr in conn.touchedEcus:
    try:
      let frame = buildUdsRequest(conn.testerAddr, ecuAddr, 0x10, [0x01'u8])
      conn.sendFrame(frame)
      discard conn.recvFrame()
    except CatchableError:
      discard

# ---------------------------------------------------------------------------
# High-level read-only diagnostic operations
# ---------------------------------------------------------------------------
type
  EcuInfo* = object
    address*: uint16
    vin*: string
    hwVersion*: string
    swVersion*: string
    supplier*: string
    serial*: string

  LiveFault* = object
    dtcCode*: string    # hex representation
    dtcRaw*: uint32
    status*: uint8
    statusText*: string
    present*: bool
    stored*: bool

proc dtcStatusText(status: uint8): string =
  var parts: seq[string]
  if (status and 0x01) != 0: parts.add "testFailed"
  if (status and 0x02) != 0: parts.add "testFailedThisMonitorCycle"
  if (status and 0x04) != 0: parts.add "pendingDTC"
  if (status and 0x08) != 0: parts.add "confirmedDTC"
  if (status and 0x10) != 0: parts.add "testNotCompleteSinceLastClear"
  if (status and 0x20) != 0: parts.add "testFailedSinceLastClear"
  if (status and 0x40) != 0: parts.add "testNotCompleteThisMonitorCycle"
  if (status and 0x80) != 0: parts.add "warningIndicatorRequested"
  if parts.len == 0: "none" else: parts.join(",")

proc readVin*(conn: EnetConnection, ecuAddr: uint16): string =
  let resp = conn.sendUds(ecuAddr, 0x22,
    [byte((DidVin shr 8) and 0xFF), byte(DidVin and 0xFF)])
  if resp.isPositive and resp.data.len >= 2:
    var vinBytes = resp.data[2 .. ^1]
    result = ""
    for b in vinBytes:
      if b >= 0x20 and b <= 0x7E:
        result.add chr(b)
  else:
    result = ""

proc readEcuInfo*(conn: EnetConnection, ecuAddr: uint16): EcuInfo =
  result.address = ecuAddr
  result.vin = conn.readVin(ecuAddr)
  # Read other identifiers, catching errors per-DID
  try:
    let resp = conn.sendUds(ecuAddr, 0x22,
      [byte((DidEcuHwVersion shr 8) and 0xFF), byte(DidEcuHwVersion and 0xFF)])
    if resp.isPositive and resp.data.len > 2:
      for b in resp.data[2 .. ^1]:
        if b >= 0x20 and b <= 0x7E: result.hwVersion.add chr(b)
  except CatchableError: discard
  try:
    let resp = conn.sendUds(ecuAddr, 0x22,
      [byte((DidEcuSwVersion shr 8) and 0xFF), byte(DidEcuSwVersion and 0xFF)])
    if resp.isPositive and resp.data.len > 2:
      for b in resp.data[2 .. ^1]:
        if b >= 0x20 and b <= 0x7E: result.swVersion.add chr(b)
  except CatchableError: discard
  try:
    let resp = conn.sendUds(ecuAddr, 0x22,
      [byte((DidSupplier shr 8) and 0xFF), byte(DidSupplier and 0xFF)])
    if resp.isPositive and resp.data.len > 2:
      for b in resp.data[2 .. ^1]:
        if b >= 0x20 and b <= 0x7E: result.supplier.add chr(b)
  except CatchableError: discard
  try:
    let resp = conn.sendUds(ecuAddr, 0x22,
      [byte((DidEcuSerial shr 8) and 0xFF), byte(DidEcuSerial and 0xFF)])
    if resp.isPositive and resp.data.len > 2:
      for b in resp.data[2 .. ^1]:
        if b >= 0x20 and b <= 0x7E: result.serial.add chr(b)
  except CatchableError: discard

proc readFaults*(conn: EnetConnection, ecuAddr: uint16): seq[LiveFault] =
  # First get DTC count
  let countResp = conn.sendUds(ecuAddr, 0x19, [0x01'u8, 0xFF'u8])
  if not countResp.isPositive:
    return @[]
  # Now get actual DTCs
  let dtcResp = conn.sendUds(ecuAddr, 0x19, [0x02'u8, 0xFF'u8])
  if not dtcResp.isPositive:
    return @[]
  let parsed = parseDtcResponse(dtcResp)
  for dtc in parsed.dtcs:
    result.add LiveFault(
      dtcCode: &"{dtc.code:06X}",
      dtcRaw: dtc.code,
      status: dtc.status,
      statusText: dtcStatusText(dtc.status),
      present: (dtc.status and 0x01) != 0,
      stored: (dtc.status and 0x08) != 0,
    )

proc readDataById*(conn: EnetConnection, ecuAddr: uint16, did: uint16): seq[byte] =
  let resp = conn.sendUds(ecuAddr, 0x22,
    [byte((did shr 8) and 0xFF), byte(did and 0xFF)])
  if resp.isPositive and resp.data.len > 2:
    result = resp.data[2 .. ^1]
  else:
    result = @[]

# ---------------------------------------------------------------------------
# Well-known F-series ECU addresses
# ---------------------------------------------------------------------------
const KnownEcus* = {
  0x00'u16: "DME (Engine)",
  0x12'u16: "EGS (Transmission)",
  0x18'u16: "DSC (Stability Control)",
  0x21'u16: "FEM (Front Electronics Module)",
  0x28'u16: "REM (Rear Electronics Module)",
  0x30'u16: "KOMBI (Instrument Cluster)",
  0x3C'u16: "SAS (Steering Angle Sensor)",
  0x40'u16: "HU_NBT (Head Unit)",
  0x44'u16: "CAS (Car Access System)",
  0x56'u16: "ACSM (Airbag)",
  0x60'u16: "ZGM (Central Gateway)",
  0x63'u16: "BDC (Body Domain Controller)",
  0x72'u16: "EPS (Electric Power Steering)",
  0x78'u16: "ICM (Integrated Chassis Management)",
}

proc scanEcus*(conn: EnetConnection, addresses: openArray[uint16] = []): seq[EcuInfo] =
  let addrs = if addresses.len > 0: @addresses
              else: (block:
                var a: seq[uint16]
                for (addr, _) in KnownEcus:
                  a.add addr
                a)
  for addr in addrs:
    try:
      let info = conn.readEcuInfo(addr)
      if info.vin.len > 0 or info.hwVersion.len > 0:
        result.add info
    except CatchableError:
      discard  # ECU not present or not responding

# ---------------------------------------------------------------------------
# JSON output (for Go orchestrator integration)
# ---------------------------------------------------------------------------
proc ecuInfoToJson(info: EcuInfo): JsonNode =
  %*{
    "address": info.address,
    "address_hex": &"0x{info.address:02X}",
    "vin": info.vin,
    "hw_version": info.hwVersion,
    "sw_version": info.swVersion,
    "supplier": info.supplier,
    "serial": info.serial,
  }

proc faultToJson(f: LiveFault): JsonNode =
  %*{
    "dtc_code": f.dtcCode,
    "dtc_raw": f.dtcRaw,
    "status": f.status,
    "status_text": f.statusText,
    "present": f.present,
    "stored": f.stored,
  }

proc outputJson(node: JsonNode) =
  stdout.write($node)
  stdout.write("\n")

proc outputPretty(node: JsonNode) =
  stdout.write(pretty(node))
  stdout.write("\n")

# ---------------------------------------------------------------------------
# CLI
# ---------------------------------------------------------------------------
type
  Command = enum
    cmdFaults, cmdEcus, cmdData, cmdVin, cmdHelp

  CliOptions = object
    command: Command
    host: string
    port: int
    ecuAddr: uint16
    did: uint16
    jsonOutput: bool
    testerAddr: uint16
    force: bool

proc printUsage() =
  echo "ista-enet: Read-only BMW ENET/HSFZ diagnostic client"
  echo ""
  echo "SAFETY:"
  echo "  - Write operations are hard-blocked at the protocol layer."
  echo "    This tool can NEVER modify car systems."
  echo "  - Will NOT connect if ISTA is running. ISTA owns the diagnostic"
  echo "    session — connecting simultaneously could corrupt its comms."
  echo "  - Returns all ECUs to default session on disconnect (cleanup)."
  echo "  - Uses tester address 0xF5 (ISTA uses 0xF4) to avoid conflicts."
  echo ""
  echo "Usage:"
  echo "  ista-enet faults [--ecu 0x00] [--host 169.254.0.10] [--json]"
  echo "  ista-enet ecus [--host 169.254.0.10] [--json]"
  echo "  ista-enet data --ecu 0x00 --did 0xF190 [--json]"
  echo "  ista-enet vin [--ecu 0x00] [--json]"
  echo ""
  echo "Commands:"
  echo "  faults    Read fault codes (DTCs) from an ECU"
  echo "  ecus      Scan for available ECUs and read their identity"
  echo "  data      Read a specific data identifier (DID) from an ECU"
  echo "  vin       Read the VIN from an ECU"
  echo ""
  echo "Options:"
  echo "  --host <ip>       Car IP address (default: 169.254.0.10)"
  echo "  --port <port>     ENET port (default: 6801)"
  echo "  --ecu <addr>      ECU address in hex (default: 0x00 = DME)"
  echo "  --did <id>        Data identifier in hex (for 'data' command)"
  echo "  --tester <addr>   Tester address in hex (default: 0xF5)"
  echo "  --json, -j        JSON output (for orchestrator integration)"
  echo "  --force            Connect even if ISTA is running (NOT RECOMMENDED)"
  echo "  --help, -h        Show this help"

proc parseHex(s: string): uint16 =
  let clean = if s.startsWith("0x") or s.startsWith("0X"): s[2..^1] else: s
  result = parseHexInt(clean).uint16

proc parseCli(): CliOptions =
  result.host = DefaultHost
  result.port = DefaultPort
  result.ecuAddr = 0x00
  result.did = DidVin
  result.testerAddr = DefaultTesterAddr
  result.command = cmdHelp
  result.force = false

  var p = initOptParser(commandLineParams())
  var positionalDone = false

  while true:
    p.next()
    case p.kind
    of cmdEnd: break
    of cmdArgument:
      if not positionalDone:
        positionalDone = true
        case p.key.toLowerAscii
        of "faults", "fault", "dtc", "dtcs": result.command = cmdFaults
        of "ecus", "ecu", "scan": result.command = cmdEcus
        of "data", "read", "did": result.command = cmdData
        of "vin": result.command = cmdVin
        of "help": result.command = cmdHelp
        else:
          echo &"Unknown command: {p.key}"
          result.command = cmdHelp
    of cmdLongOption, cmdShortOption:
      case p.key.toLowerAscii
      of "host", "h" & "ost": result.host = p.val  # avoid short-option collision
      of "port": result.port = parseInt(p.val)
      of "ecu": result.ecuAddr = parseHex(p.val)
      of "did": result.did = parseHex(p.val)
      of "tester": result.testerAddr = parseHex(p.val)
      of "json", "j": result.jsonOutput = true
      of "force": result.force = true
      of "help", "h":
        result.command = cmdHelp
      else: discard

proc runFaults(opts: CliOptions) =
  let conn = newEnetConnection(opts.host, opts.port, opts.testerAddr)
  try:
    conn.connect(opts.force)
    conn.doHandshake()
    discard conn.sendUds(opts.ecuAddr, 0x10, [0x03'u8])  # extended session
    let faults = conn.readFaults(opts.ecuAddr)
    if opts.jsonOutput:
      var arr = newJArray()
      for f in faults: arr.add faultToJson(f)
      outputJson(%*{"ecu": &"0x{opts.ecuAddr:02X}", "faults": arr, "count": faults.len})
    else:
      echo &"Faults from ECU 0x{opts.ecuAddr:02X}: {faults.len} found"
      for f in faults:
        let marker = if f.present: "[ACTIVE]" else: "[STORED]"
        echo &"  {marker} DTC 0x{f.dtcCode} — {f.statusText}"
  finally:
    conn.disconnect()

proc runEcus(opts: CliOptions) =
  let conn = newEnetConnection(opts.host, opts.port, opts.testerAddr)
  try:
    conn.connect(opts.force)
    conn.doHandshake()
    let ecus = conn.scanEcus()
    if opts.jsonOutput:
      var arr = newJArray()
      for e in ecus: arr.add ecuInfoToJson(e)
      outputJson(%*{"ecus": arr, "count": ecus.len})
    else:
      echo &"Found {ecus.len} ECUs:"
      for e in ecus:
        var label = &"0x{e.address:02X}"
        for (addr, name) in KnownEcus:
          if addr == e.address:
            label = &"{name} (0x{e.address:02X})"
            break
        echo &"  {label}"
        if e.vin.len > 0: echo &"    VIN: {e.vin}"
        if e.hwVersion.len > 0: echo &"    HW:  {e.hwVersion}"
        if e.swVersion.len > 0: echo &"    SW:  {e.swVersion}"
        if e.supplier.len > 0: echo &"    Supplier: {e.supplier}"
  finally:
    conn.disconnect()

proc runData(opts: CliOptions) =
  let conn = newEnetConnection(opts.host, opts.port, opts.testerAddr)
  try:
    conn.connect(opts.force)
    conn.doHandshake()
    discard conn.sendUds(opts.ecuAddr, 0x10, [0x03'u8])
    let data = conn.readDataById(opts.ecuAddr, opts.did)
    if opts.jsonOutput:
      var hexParts: seq[string]
      for b in data: hexParts.add &"{b:02X}"
      outputJson(%*{
        "ecu": &"0x{opts.ecuAddr:02X}",
        "did": &"0x{opts.did:04X}",
        "data_hex": hexParts.join(" "),
        "data_ascii": (block:
          var s = ""
          for b in data:
            if b >= 0x20 and b <= 0x7E: s.add chr(b)
            else: s.add '.'
          s),
        "length": data.len,
      })
    else:
      echo &"DID 0x{opts.did:04X} from ECU 0x{opts.ecuAddr:02X}:"
      var hexLine = ""
      var asciiLine = ""
      for b in data:
        hexLine.add &"{b:02X} "
        if b >= 0x20 and b <= 0x7E: asciiLine.add chr(b)
        else: asciiLine.add '.'
      echo &"  Hex:   {hexLine}"
      echo &"  ASCII: {asciiLine}"
  finally:
    conn.disconnect()

proc runVin(opts: CliOptions) =
  let conn = newEnetConnection(opts.host, opts.port, opts.testerAddr)
  try:
    conn.connect(opts.force)
    conn.doHandshake()
    let vin = conn.readVin(opts.ecuAddr)
    if opts.jsonOutput:
      outputJson(%*{"vin": vin, "ecu": &"0x{opts.ecuAddr:02X}"})
    else:
      if vin.len > 0:
        echo &"VIN: {vin}"
      else:
        echo "Could not read VIN from this ECU"
  finally:
    conn.disconnect()

when isMainModule:
  let opts = parseCli()
  case opts.command
  of cmdHelp: printUsage()
  of cmdFaults: runFaults(opts)
  of cmdEcus: runEcus(opts)
  of cmdData: runData(opts)
  of cmdVin: runVin(opts)
