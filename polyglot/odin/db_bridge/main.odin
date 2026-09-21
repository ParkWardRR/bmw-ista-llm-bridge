package db_bridge

// ista-keyextract: Parses .NET PE/CLI metadata from a DLL/EXE file to extract
// the assembly public key token, which is the decryption password for
// BMW ISTA's DiagDocDb.sqlite.
//
// Usage:
//   ista-keyextract --dll C:\path\to\file.dll
//   ista-keyextract --keyfile C:\path\to\keys.txt
//   ista-keyextract   (uses default path)

import "core:fmt"
import "core:os"
import "core:strings"
import sha1 "core:crypto/legacy/sha1"

DEFAULT_DLL_PATH :: `C:\EC-Apps\ISTA\TesterGUI\bin\Release\ISTAGUI.exe`

read_u16 :: proc(data: []u8, o: int) -> u16 {
	return u16(data[o]) | u16(data[o + 1]) << 8
}

read_u32 :: proc(data: []u8, o: int) -> u32 {
	return u32(data[o]) | u32(data[o + 1]) << 8 | u32(data[o + 2]) << 16 | u32(data[o + 3]) << 24
}

read_u64 :: proc(data: []u8, o: int) -> u64 {
	lo := u64(read_u32(data, o))
	hi := u64(read_u32(data, o + 4))
	return lo | hi << 32
}

Section :: struct {
	virtual_address:    u32,
	virtual_size:       u32,
	pointer_to_raw_data: u32,
	size_of_raw_data:   u32,
}

rva_to_offset :: proc(sections: []Section, rva: u32) -> (int, bool) {
	for s in sections {
		// Use max of virtual_size / size_of_raw_data to be safe against
		// virtual_size == 0 in some tools' output.
		size := s.virtual_size
		if size < s.size_of_raw_data {
			size = s.size_of_raw_data
		}
		if rva >= s.virtual_address && rva < s.virtual_address + size {
			return int(s.pointer_to_raw_data + (rva - s.virtual_address)), true
		}
	}
	return 0, false
}

read_blob_length :: proc(data: []u8, o: int) -> (length: int, header_size: int, ok: bool) {
	if o >= len(data) {
		return 0, 0, false
	}
	b0 := data[o]
	if b0 & 0x80 == 0 {
		return int(b0 & 0x7F), 1, true
	} else if b0 & 0xC0 == 0x80 {
		if o + 1 >= len(data) {
			return 0, 0, false
		}
		length = (int(b0 & 0x3F) << 8) | int(data[o + 1])
		return length, 2, true
	} else if b0 & 0xE0 == 0xC0 {
		if o + 3 >= len(data) {
			return 0, 0, false
		}
		length = (int(b0 & 0x1F) << 24) | (int(data[o + 1]) << 16) | (int(data[o + 2]) << 8) | int(data[o + 3])
		return length, 4, true
	}
	return 0, 0, false
}

extract_public_key_token :: proc(path: string) -> (token: string, err: string) {
	data, read_err := os.read_entire_file(path, context.allocator)
	if read_err != nil {
		return "", fmt.tprintf("failed to read file: %s", path)
	}
	defer delete(data)

	if len(data) < 0x40 {
		return "", "file too small to be a valid PE"
	}

	// DOS header
	if data[0] != 'M' || data[1] != 'Z' {
		return "", "not a valid PE file (missing MZ signature)"
	}
	e_lfanew := int(read_u32(data, 0x3C))
	if e_lfanew <= 0 || e_lfanew + 24 > len(data) {
		return "", "invalid e_lfanew offset"
	}

	// PE signature
	pe_sig := read_u32(data, e_lfanew)
	if pe_sig != 0x00004550 {
		return "", "invalid PE signature"
	}

	coff_start := e_lfanew + 4
	number_of_sections := int(read_u16(data, coff_start + 2))
	size_of_optional_header := int(read_u16(data, coff_start + 16))

	optional_header_start := coff_start + 20
	if optional_header_start + 2 > len(data) {
		return "", "file truncated at optional header"
	}
	magic := read_u16(data, optional_header_start)

	clr_dir_offset: int
	if magic == 0x10B {
		// PE32
		clr_dir_offset = optional_header_start + 208
	} else if magic == 0x20B {
		// PE32+
		clr_dir_offset = optional_header_start + 224
	} else {
		return "", fmt.tprintf("unsupported optional header magic: 0x%X", magic)
	}

	if clr_dir_offset + 8 > len(data) {
		return "", "file truncated at CLR data directory"
	}
	clr_rva := read_u32(data, clr_dir_offset)
	if clr_rva == 0 {
		return "", "this file is not a .NET assembly (no CLR data directory)"
	}

	// Section headers
	section_headers_start := optional_header_start + size_of_optional_header
	sections := make([]Section, number_of_sections)
	defer delete(sections)
	for i := 0; i < number_of_sections; i += 1 {
		base := section_headers_start + i * 40
		if base + 40 > len(data) {
			return "", "file truncated at section headers"
		}
		sections[i] = Section {
			virtual_size        = read_u32(data, base + 8),
			virtual_address     = read_u32(data, base + 12),
			size_of_raw_data    = read_u32(data, base + 16),
			pointer_to_raw_data = read_u32(data, base + 20),
		}
	}

	clr_header_offset, clr_ok := rva_to_offset(sections, clr_rva)
	if !clr_ok {
		return "", "could not resolve CLR header RVA to file offset"
	}

	// IMAGE_COR20_HEADER: MetaData directory is at offset 8 (RVA u32, Size u32)
	if clr_header_offset + 16 > len(data) {
		return "", "file truncated at CLR header"
	}
	metadata_rva := read_u32(data, clr_header_offset + 8)

	metadata_offset, md_ok := rva_to_offset(sections, metadata_rva)
	if !md_ok {
		return "", "could not resolve metadata RVA to file offset"
	}

	// Metadata root
	if metadata_offset + 16 > len(data) {
		return "", "file truncated at metadata root"
	}
	md_sig := read_u32(data, metadata_offset)
	if md_sig != 0x424A5342 {
		return "", "invalid metadata root signature"
	}

	version_length := int(read_u32(data, metadata_offset + 12))
	version_string_start := metadata_offset + 16
	padded_len := (version_length + 3) & ~int(3)
	after_version := version_string_start + padded_len

	if after_version + 4 > len(data) {
		return "", "file truncated after version string"
	}
	// Flags (u16) then NumberOfStreams (u16)
	number_of_streams := int(read_u16(data, after_version + 2))
	stream_header_start := after_version + 4

	strings_stream_offset := -1
	strings_stream_size := 0
	blob_stream_offset := -1
	blob_stream_size := 0
	tables_stream_offset := -1
	tables_stream_size := 0

	pos := stream_header_start
	for i := 0; i < number_of_streams; i += 1 {
		if pos + 8 > len(data) {
			return "", "file truncated in stream headers"
		}
		stream_offset := int(read_u32(data, pos))
		stream_size := int(read_u32(data, pos + 4))
		name_start := pos + 8
		// Read null-terminated name
		name_end := name_start
		for name_end < len(data) && data[name_end] != 0 {
			name_end += 1
		}
		name := string(data[name_start:name_end])

		switch name {
		case "#~", "#-":
			tables_stream_offset = metadata_offset + stream_offset
			tables_stream_size = stream_size
		case "#Strings":
			strings_stream_offset = metadata_offset + stream_offset
			strings_stream_size = stream_size
		case "#Blob":
			blob_stream_offset = metadata_offset + stream_offset
			blob_stream_size = stream_size
		}

		// Name is padded to 4-byte boundary, including the null terminator
		name_len_with_null := (name_end - name_start) + 1
		padded_name_len := (name_len_with_null + 3) & ~int(3)
		pos = name_start + padded_name_len
	}

	if tables_stream_offset < 0 {
		return "", "could not find #~ (tables) stream"
	}
	if blob_stream_offset < 0 {
		return "", "could not find #Blob stream"
	}
	_ = strings_stream_offset
	_ = strings_stream_size
	_ = tables_stream_size

	// #~ stream header
	t := tables_stream_offset
	if t + 24 > len(data) {
		return "", "file truncated in tables stream header"
	}
	heap_sizes := data[t + 6]
	valid := read_u64(data, t + 8)
	// sorted := read_u64(data, t + 16) // not needed

	string_index_size := (heap_sizes & 0x01) != 0 ? 4 : 2
	guid_index_size := (heap_sizes & 0x02) != 0 ? 4 : 2
	blob_index_size := (heap_sizes & 0x04) != 0 ? 4 : 2

	row_counts_start := t + 24
	num_tables := 64
	row_counts := make([]u32, num_tables)
	defer delete(row_counts)

	rc_pos := row_counts_start
	for i := 0; i < num_tables; i += 1 {
		if (valid & (u64(1) << uint(i))) != 0 {
			if rc_pos + 4 > len(data) {
				return "", "file truncated in table row counts"
			}
			row_counts[i] = read_u32(data, rc_pos)
			rc_pos += 4
		} else {
			row_counts[i] = 0
		}
	}

	rows_data_start := rc_pos

	// Table indices we need for computing row sizes / offsets.
	MODULE :: 0x00
	TYPEREF :: 0x01
	TYPEDEF :: 0x02
	FIELD :: 0x04
	METHODDEF :: 0x06
	PARAM :: 0x08
	INTERFACEIMPL :: 0x09
	MEMBERREF :: 0x0A
	CONSTANT :: 0x0B
	CUSTOMATTRIBUTE :: 0x0C
	FIELDMARSHAL :: 0x0D
	DECLSECURITY :: 0x0E
	CLASSLAYOUT :: 0x0F
	FIELDLAYOUT :: 0x10
	STANDALONESIG :: 0x11
	EVENTMAP :: 0x12
	EVENT :: 0x14
	PROPERTYMAP :: 0x15
	PROPERTY :: 0x17
	METHODSEMANTICS :: 0x18
	METHODIMPL :: 0x19
	MODULEREF :: 0x1A
	TYPESPEC :: 0x1B
	IMPLMAP :: 0x1C
	FIELDRVA :: 0x1D
	ASSEMBLY :: 0x20
	ASSEMBLYPROCESSOR :: 0x21
	ASSEMBLYOS :: 0x22
	ASSEMBLYREF :: 0x23
	ASSEMBLYREFPROCESSOR :: 0x24
	ASSEMBLYREFOS :: 0x25
	FILE :: 0x26
	EXPORTEDTYPE :: 0x27
	MANIFESTRESOURCE :: 0x28
	NESTEDCLASS :: 0x29
	GENERICPARAM :: 0x2A
	METHODSPEC :: 0x2B
	GENERICPARAMCONSTRAINT :: 0x2C

	if row_counts[ASSEMBLY] == 0 {
		return "", "no Assembly table row found (not an assembly manifest module)"
	}

	// Coded index helper: given the list of tables that participate and the
	// number of bits used for the tag, compute the index size (2 or 4 bytes).
	coded_index_size :: proc(row_counts: []u32, tables: []int, tag_bits: uint) -> int {
		max_rows: u32 = 0
		for tbl in tables {
			if row_counts[tbl] > max_rows {
				max_rows = row_counts[tbl]
			}
		}
		limit := u32(1) << (16 - tag_bits)
		if max_rows > limit {
			return 4
		}
		return 2
	}

	simple_index_size :: proc(row_counts: []u32, tbl: int) -> int {
		if row_counts[tbl] > 0xFFFF {
			return 4
		}
		return 2
	}

	// Coded index table groups (per ECMA-335 II.24.2.6)
	TypeDefOrRef := []int{TYPEDEF, TYPEREF, TYPESPEC}
	HasConstant := []int{FIELD, PARAM, PROPERTY}
	HasCustomAttribute := []int{
		METHODDEF, FIELD, TYPEREF, TYPEDEF, PARAM, INTERFACEIMPL, MEMBERREF,
		MODULE, DECLSECURITY, PROPERTY, EVENT, STANDALONESIG, MODULEREF,
		TYPESPEC, ASSEMBLY, ASSEMBLYREF, FILE, EXPORTEDTYPE, MANIFESTRESOURCE,
		GENERICPARAM, GENERICPARAMCONSTRAINT, METHODSPEC,
	}
	HasFieldMarshal := []int{FIELD, PARAM}
	HasDeclSecurity := []int{TYPEDEF, METHODDEF, ASSEMBLY}
	MemberRefParent := []int{TYPEDEF, TYPEREF, MODULEREF, METHODDEF, TYPESPEC}
	HasSemantics := []int{EVENT, PROPERTY}
	MethodDefOrRef := []int{METHODDEF, MEMBERREF}
	MemberForwarded := []int{FIELD, METHODDEF}
	Implementation := []int{FILE, ASSEMBLYREF, EXPORTEDTYPE}
	CustomAttributeType := []int{METHODDEF, MEMBERREF} // simplification: only 2 real tag bits used, but tag is 3 bits
	ResolutionScope := []int{MODULE, MODULEREF, ASSEMBLYREF, TYPEREF}
	TypeOrMethodDef := []int{TYPEDEF, METHODDEF}

	type_def_or_ref_size := coded_index_size(row_counts, TypeDefOrRef, 2)
	has_constant_size := coded_index_size(row_counts, HasConstant, 2)
	has_custom_attribute_size := coded_index_size(row_counts, HasCustomAttribute, 5)
	has_field_marshal_size := coded_index_size(row_counts, HasFieldMarshal, 1)
	has_decl_security_size := coded_index_size(row_counts, HasDeclSecurity, 2)
	member_ref_parent_size := coded_index_size(row_counts, MemberRefParent, 3)
	has_semantics_size := coded_index_size(row_counts, HasSemantics, 1)
	method_def_or_ref_size := coded_index_size(row_counts, MethodDefOrRef, 1)
	member_forwarded_size := coded_index_size(row_counts, MemberForwarded, 1)
	implementation_size := coded_index_size(row_counts, Implementation, 2)
	custom_attribute_type_size := coded_index_size(row_counts, CustomAttributeType, 3)
	resolution_scope_size := coded_index_size(row_counts, ResolutionScope, 2)
	type_or_method_def_size := coded_index_size(row_counts, TypeOrMethodDef, 1)

	// Row size calculator for each table index (only tables that can precede
	// Assembly (0x20) in table numbering need to be accurate: 0x00-0x1F).
	row_size :: proc(
		tbl: int,
		row_counts: []u32,
		string_index_size, guid_index_size, blob_index_size: int,
		type_def_or_ref_size, has_constant_size, has_custom_attribute_size: int,
		has_field_marshal_size, has_decl_security_size, member_ref_parent_size: int,
		has_semantics_size, method_def_or_ref_size, member_forwarded_size: int,
		implementation_size, custom_attribute_type_size, resolution_scope_size: int,
		type_or_method_def_size: int,
	) -> int {
		switch tbl {
		case 0x00: // Module: Generation(2) Name(str) Mvid(guid) EncId(guid) EncBaseId(guid)
			return 2 + string_index_size + guid_index_size * 3
		case 0x01: // TypeRef: ResolutionScope(coded) Name(str) Namespace(str)
			return resolution_scope_size + string_index_size * 2
		case 0x02: // TypeDef: Flags(4) Name(str) Namespace(str) Extends(coded) FieldList(simple) MethodList(simple)
			return 4 + string_index_size * 2 + type_def_or_ref_size + simple_index_size(row_counts, FIELD) + simple_index_size(row_counts, METHODDEF)
		case 0x03: // (reserved / not used)
			return 0
		case 0x04: // Field: Flags(2) Name(str) Signature(blob)
			return 2 + string_index_size + blob_index_size
		case 0x05:
			return 0
		case 0x06: // MethodDef: RVA(4) ImplFlags(2) Flags(2) Name(str) Signature(blob) ParamList(simple)
			return 4 + 2 + 2 + string_index_size + blob_index_size + simple_index_size(row_counts, PARAM)
		case 0x07:
			return 0
		case 0x08: // Param: Flags(2) Sequence(2) Name(str)
			return 2 + 2 + string_index_size
		case 0x09: // InterfaceImpl: Class(simple TypeDef) Interface(coded TypeDefOrRef)
			return simple_index_size(row_counts, TYPEDEF) + type_def_or_ref_size
		case 0x0A: // MemberRef: Class(coded) Name(str) Signature(blob)
			return member_ref_parent_size + string_index_size + blob_index_size
		case 0x0B: // Constant: Type(2, 1byte+1pad actually 2 bytes incl padding) Parent(coded) Value(blob)
			return 2 + has_constant_size + blob_index_size
		case 0x0C: // CustomAttribute: Parent(coded) Type(coded) Value(blob)
			return has_custom_attribute_size + custom_attribute_type_size + blob_index_size
		case 0x0D: // FieldMarshal: Parent(coded) NativeType(blob)
			return has_field_marshal_size + blob_index_size
		case 0x0E: // DeclSecurity: Action(2) Parent(coded) PermissionSet(blob)
			return 2 + has_decl_security_size + blob_index_size
		case 0x0F: // ClassLayout: PackingSize(2) ClassSize(4) Parent(simple TypeDef)
			return 2 + 4 + simple_index_size(row_counts, TYPEDEF)
		case 0x10: // FieldLayout: Offset(4) Field(simple)
			return 4 + simple_index_size(row_counts, FIELD)
		case 0x11: // StandAloneSig: Signature(blob)
			return blob_index_size
		case 0x12: // EventMap: Parent(simple TypeDef) EventList(simple Event)
			return simple_index_size(row_counts, TYPEDEF) + simple_index_size(row_counts, EVENT)
		case 0x13:
			return 0
		case 0x14: // Event: EventFlags(2) Name(str) EventType(coded TypeDefOrRef)
			return 2 + string_index_size + type_def_or_ref_size
		case 0x15: // PropertyMap: Parent(simple TypeDef) PropertyList(simple Property)
			return simple_index_size(row_counts, TYPEDEF) + simple_index_size(row_counts, PROPERTY)
		case 0x16:
			return 0
		case 0x17: // Property: Flags(2) Name(str) Type(blob)
			return 2 + string_index_size + blob_index_size
		case 0x18: // MethodSemantics: Semantics(2) Method(simple MethodDef) Association(coded HasSemantics)
			return 2 + simple_index_size(row_counts, METHODDEF) + has_semantics_size
		case 0x19: // MethodImpl: Class(simple TypeDef) MethodBody(coded MethodDefOrRef) MethodDeclaration(coded MethodDefOrRef)
			return simple_index_size(row_counts, TYPEDEF) + method_def_or_ref_size * 2
		case 0x1A: // ModuleRef: Name(str)
			return string_index_size
		case 0x1B: // TypeSpec: Signature(blob)
			return blob_index_size
		case 0x1C: // ImplMap: MappingFlags(2) MemberForwarded(coded) ImportName(str) ImportScope(simple ModuleRef)
			return 2 + member_forwarded_size + string_index_size + simple_index_size(row_counts, MODULEREF)
		case 0x1D: // FieldRVA: RVA(4) Field(simple Field)
			return 4 + simple_index_size(row_counts, FIELD)
		case 0x1E:
			return 0
		case 0x1F:
			return 0
		case 0x20: // Assembly
			return 4 + 2 + 2 + 2 + 2 + 4 + blob_index_size + string_index_size * 2
		}
		return 0
	}

	// Sum up sizes of all tables preceding Assembly (0x20) to find its offset.
	offset := rows_data_start
	for tbl := 0; tbl < ASSEMBLY; tbl += 1 {
		count := row_counts[tbl]
		if count == 0 {
			continue
		}
		rsize := row_size(
			tbl, row_counts, string_index_size, guid_index_size, blob_index_size,
			type_def_or_ref_size, has_constant_size, has_custom_attribute_size,
			has_field_marshal_size, has_decl_security_size, member_ref_parent_size,
			has_semantics_size, method_def_or_ref_size, member_forwarded_size,
			implementation_size, custom_attribute_type_size, resolution_scope_size,
			type_or_method_def_size,
		)
		offset += int(count) * rsize
	}

	// Now `offset` points to the start of the Assembly table (row 0).
	// Assembly row layout:
	//   HashAlgId(4) MajorVersion(2) MinorVersion(2) BuildNumber(2) RevisionNumber(2)
	//   Flags(4) PublicKey(blob) Name(str) Culture(str)
	public_key_field_offset := offset + 4 + 2 + 2 + 2 + 2 + 4
	if public_key_field_offset + blob_index_size > len(data) {
		return "", "file truncated at Assembly table PublicKey field"
	}

	blob_index: u32
	if blob_index_size == 4 {
		blob_index = read_u32(data, public_key_field_offset)
	} else {
		blob_index = u32(read_u16(data, public_key_field_offset))
	}

	if blob_index == 0 {
		return "", "Assembly table has no public key (unsigned assembly)"
	}

	blob_entry_offset := blob_stream_offset + int(blob_index)
	blob_len, header_size, blob_ok := read_blob_length(data, blob_entry_offset)
	if !blob_ok {
		return "", "failed to read public key blob length"
	}
	blob_data_start := blob_entry_offset + header_size
	if blob_data_start + blob_len > len(data) {
		return "", "file truncated at public key blob data"
	}
	public_key_blob := data[blob_data_start:blob_data_start + blob_len]

	// Compute SHA-1 of the public key blob.
	digest: [sha1.DIGEST_SIZE]u8
	ctx: sha1.Context
	sha1.init(&ctx)
	sha1.update(&ctx, public_key_blob)
	sha1.final(&ctx, digest[:])

	// Take last 8 bytes of the SHA-1 hash, reversed, as the public key token.
	last8 := digest[sha1.DIGEST_SIZE - 8:]
	token_bytes: [8]u8
	for i := 0; i < 8; i += 1 {
		token_bytes[i] = last8[7 - i]
	}

	sb := strings.builder_make()
	defer strings.builder_destroy(&sb)
	for b in token_bytes {
		fmt.sbprintf(&sb, "%02X", b)
	}

	return strings.clone(strings.to_string(sb)), ""
}

read_keyfile :: proc(path: string) -> (token: string, err: string) {
	data, read_err := os.read_entire_file(path, context.allocator)
	if read_err != nil {
		return "", fmt.tprintf("failed to read keyfile: %s", path)
	}
	defer delete(data)

	content := string(data)
	if len(strings.trim_space(content)) == 0 {
		return "", "keyfile is empty"
	}

	marker :: "DiagDocDb SQLite SEE Password:"
	if idx := strings.index(content, marker); idx >= 0 {
		after := content[idx + len(marker):]
		after = strings.trim_left(after, " \t\r\n")
		end := strings.index_any(after, "\r\n")
		if end < 0 {
			end = len(after)
		}
		pw := strings.trim_space(after[:end])
		if len(pw) > 0 {
			return strings.clone(strings.to_upper(pw)), ""
		}
	}

	return "", "could not find 'DiagDocDb SQLite SEE Password:' in keyfile"
}

json_escape :: proc(s: string) -> string {
	sb := strings.builder_make()
	for r in s {
		switch r {
		case '"':
			strings.write_string(&sb, "\\\"")
		case '\\':
			strings.write_string(&sb, "\\\\")
		case:
			strings.write_rune(&sb, r)
		}
	}
	return strings.to_string(sb)
}

main :: proc() {
	args := os.args[1:]

	mode := "dll"
	path := DEFAULT_DLL_PATH

	i := 0
	for i < len(args) {
		switch args[i] {
		case "--dll":
			if i + 1 >= len(args) {
				fmt.eprintln("Error: --dll requires a path argument")
				os.exit(1)
			}
			mode = "dll"
			path = args[i + 1]
			i += 2
		case "--keyfile":
			if i + 1 >= len(args) {
				fmt.eprintln("Error: --keyfile requires a path argument")
				os.exit(1)
			}
			mode = "keyfile"
			path = args[i + 1]
			i += 2
		case:
			fmt.eprintf("Error: unrecognized argument '%s'\n", args[i])
			os.exit(1)
		}
	}

	token: string
	err: string

	if mode == "keyfile" {
		token, err = read_keyfile(path)
	} else {
		token, err = extract_public_key_token(path)
	}

	if err != "" {
		fmt.eprintf("Error: %s\n", err)
		os.exit(1)
	}

	fmt.printf(
		`{{"source":"%s","path":"%s","public_key_token":"%s","db_password":"%s"}}`,
		mode == "keyfile" ? "keyfile" : "dll",
		json_escape(path),
		token,
		token,
	)
	fmt.println()
}
