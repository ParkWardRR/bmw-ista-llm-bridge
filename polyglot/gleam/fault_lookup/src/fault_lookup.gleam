import argv
import gleam/dynamic
import gleam/int
import gleam/io
import gleam/json
import gleam/list
import gleam/result
import gleam/string
import simplifile

/// A single BMW fault code entry, as found in `faultcodes.json`.
pub type FaultCode {
  FaultCode(
    code: String,
    sae_code: String,
    title: String,
    ecu_variant: String,
    ecu_group: String,
    weighting: Int,
    safety_relevant: Bool,
  )
}

/// A single OBD P-code entry, as found in `pcodes.json`. Each P-code maps to
/// a BMW fault code number (`fcode`) that can be cross referenced against the
/// `FaultCode` list.
pub type PCode {
  PCode(pcode: String, fcode: Int, device: String, title: String)
}

pub fn main() {
  let args = argv.load().arguments
  case args {
    [] -> print_usage()
    ["--json", ..rest] -> run_search_json(string.join(rest, " "))
    _ -> {
      let #(json_mode, search_args) = partition_json_flag(args)
      case json_mode {
        True -> run_search_json(string.join(search_args, " "))
        False -> run_search(string.join(search_args, " "))
      }
    }
  }
}

fn partition_json_flag(args: List(String)) -> #(Bool, List(String)) {
  let filtered = list.filter(args, fn(a) { a != "--json" && a != "-j" })
  let has_json = list.length(filtered) != list.length(args)
  #(has_json, filtered)
}

fn print_usage() -> Nil {
  io.println("BMW Fault Code Lookup")
  io.println("")
  io.println("Usage: gleam run -- <search term>")
  io.println("")
  io.println(
    "Searches BMW fault codes and OBD P-codes by code number, SAE code,",
  )
  io.println("P-code, fault code number, or description text (case-insensitive,")
  io.println("partial match).")
}

fn run_search(query: String) -> Nil {
  case resolve_data_dir() {
    Error(Nil) ->
      io.println(
        "Error: could not find a data directory (looked for ./data and ./test_data)",
      )
    Ok(dir) -> {
      case load_fault_codes(dir), load_pcodes(dir) {
        Error(message), _ -> io.println("Error loading fault codes: " <> message)
        _, Error(message) -> io.println("Error loading P-codes: " <> message)
        Ok(fault_codes), Ok(pcodes) -> {
          let matched_fault_codes =
            list.filter(fault_codes, matches_fault_code(_, query))
          let matched_pcodes = list.filter(pcodes, matches_pcode(_, query))

          print_fault_results(matched_fault_codes)
          io.println("")
          print_pcode_results(matched_pcodes)
        }
      }
    }
  }
}

fn run_search_json(query: String) -> Nil {
  case resolve_data_dir() {
    Error(Nil) -> io.println("{\"error\":\"no data directory found\"}")
    Ok(dir) -> {
      case load_fault_codes(dir), load_pcodes(dir) {
        Error(msg), _ ->
          io.println("{\"error\":\"" <> msg <> "\"}")
        _, Error(msg) ->
          io.println("{\"error\":\"" <> msg <> "\"}")
        Ok(fault_codes), Ok(pcodes) -> {
          let matched_faults =
            list.filter(fault_codes, matches_fault_code(_, query))
          let matched_pcodes = list.filter(pcodes, matches_pcode(_, query))

          let fault_json = list.map(matched_faults, fault_code_to_json)
          let pcode_json = list.map(matched_pcodes, pcode_to_json)

          let output =
            "{\"fault_codes\":["
            <> string.join(fault_json, ",")
            <> "],\"pcodes\":["
            <> string.join(pcode_json, ",")
            <> "]}"
          io.println(output)
        }
      }
    }
  }
}

fn json_string(value: String) -> String {
  "\""
  <> string.replace(
    string.replace(value, "\\", "\\\\"),
    "\"",
    "\\\"",
  )
  <> "\""
}

fn fault_code_to_json(fc: FaultCode) -> String {
  "{\"code\":"
  <> json_string(fc.code)
  <> ",\"sae_code\":"
  <> json_string(fc.sae_code)
  <> ",\"title\":"
  <> json_string(fc.title)
  <> ",\"ecu_variant\":"
  <> json_string(fc.ecu_variant)
  <> ",\"ecu_group\":"
  <> json_string(fc.ecu_group)
  <> ",\"weighting\":"
  <> int.to_string(fc.weighting)
  <> ",\"safety_relevant\":"
  <> case fc.safety_relevant {
    True -> "true"
    False -> "false"
  }
  <> "}"
}

fn pcode_to_json(pc: PCode) -> String {
  "{\"pcode\":"
  <> json_string(pc.pcode)
  <> ",\"fcode\":"
  <> int.to_string(pc.fcode)
  <> ",\"device\":"
  <> json_string(pc.device)
  <> ",\"title\":"
  <> json_string(pc.title)
  <> "}"
}

/// Prefer a `data` directory (for real/production data) but fall back to
/// `test_data` (the bundled sample data) if it does not exist, so that
/// `gleam run -- <term>` works out of the box.
fn resolve_data_dir() -> Result(String, Nil) {
  case simplifile.is_directory("data") {
    Ok(True) -> Ok("data")
    _ ->
      case simplifile.is_directory("test_data") {
        Ok(True) -> Ok("test_data")
        _ -> Error(Nil)
      }
  }
}

fn load_fault_codes(dir: String) -> Result(List(FaultCode), String) {
  let path = dir <> "/faultcodes.json"
  use content <- result.try(read_file(path))
  json.decode(from: content, using: dynamic.list(fault_code_decoder()))
  |> result.map_error(fn(_) { "could not parse " <> path <> " as JSON" })
}

fn load_pcodes(dir: String) -> Result(List(PCode), String) {
  let path = dir <> "/pcodes.json"
  use content <- result.try(read_file(path))
  json.decode(from: content, using: dynamic.list(pcode_decoder()))
  |> result.map_error(fn(_) { "could not parse " <> path <> " as JSON" })
}

fn read_file(path: String) -> Result(String, String) {
  simplifile.read(path)
  |> result.map_error(fn(error) {
    "could not read " <> path <> " (" <> simplifile.describe_error(error) <> ")"
  })
}

fn fault_code_decoder() -> dynamic.Decoder(FaultCode) {
  dynamic.decode7(
    FaultCode,
    dynamic.field("code", dynamic.string),
    dynamic.field("sae_code", dynamic.string),
    dynamic.field("title", dynamic.string),
    dynamic.field("ecu_variant", dynamic.string),
    dynamic.field("ecu_group", dynamic.string),
    dynamic.field("weighting", dynamic.int),
    dynamic.field("safety_relevant", dynamic.bool),
  )
}

fn pcode_decoder() -> dynamic.Decoder(PCode) {
  dynamic.decode4(
    PCode,
    dynamic.field("pcode", dynamic.string),
    dynamic.field("fcode", dynamic.int),
    dynamic.field("device", dynamic.string),
    dynamic.field("title", dynamic.string),
  )
}

fn matches_fault_code(fault_code: FaultCode, query: String) -> Bool {
  let needle = string.lowercase(query)
  string.contains(string.lowercase(fault_code.code), needle)
  || string.contains(string.lowercase(fault_code.sae_code), needle)
  || string.contains(string.lowercase(fault_code.title), needle)
  || string.contains(string.lowercase(fault_code.ecu_variant), needle)
  || string.contains(string.lowercase(fault_code.ecu_group), needle)
}

fn matches_pcode(pcode: PCode, query: String) -> Bool {
  let needle = string.lowercase(query)
  string.contains(string.lowercase(pcode.pcode), needle)
  || string.contains(string.lowercase(pcode.title), needle)
  || string.contains(string.lowercase(pcode.device), needle)
  || string.contains(int.to_string(pcode.fcode), needle)
}

fn print_fault_results(results: List(FaultCode)) -> Nil {
  let count = list.length(results)
  io.println(
    "=== Fault Code Results (" <> int.to_string(count) <> " " <> match_word(
      count,
    ) <> ") ===",
  )
  io.println("")
  case results {
    [] -> Nil
    _ -> io.println(string.join(list.index_map(results, format_fault_entry), "\n\n"))
  }
}

fn print_pcode_results(results: List(PCode)) -> Nil {
  let count = list.length(results)
  io.println(
    "=== P-Code Results (" <> int.to_string(count) <> " " <> match_word(count) <> ") ===",
  )
  io.println("")
  case results {
    [] -> Nil
    _ -> io.println(string.join(list.index_map(results, format_pcode_entry), "\n\n"))
  }
}

fn format_fault_entry(fault_code: FaultCode, index: Int) -> String {
  let sae = case fault_code.sae_code {
    "" -> "N/A"
    code -> code
  }
  let safety = case fault_code.safety_relevant {
    True -> "Yes"
    False -> "No"
  }

  "["
  <> int.to_string(index + 1)
  <> "] Code: "
  <> fault_code.code
  <> " | SAE: "
  <> sae
  <> " | ECU: "
  <> fault_code.ecu_variant
  <> " ("
  <> fault_code.ecu_group
  <> ")\n    "
  <> fault_code.title
  <> "\n    Weighting: "
  <> int.to_string(fault_code.weighting)
  <> " | Safety: "
  <> safety
}

fn format_pcode_entry(pcode: PCode, index: Int) -> String {
  "["
  <> int.to_string(index + 1)
  <> "] "
  <> pcode.pcode
  <> " -> F"
  <> int.to_string(pcode.fcode)
  <> " | Device: "
  <> pcode.device
  <> "\n    "
  <> pcode.title
}

fn match_word(count: Int) -> String {
  case count {
    1 -> "match"
    _ -> "matches"
  }
}
