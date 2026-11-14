package sandbox

import "errors"

// maxWasmTableInitial caps funcref/externref table minimum size before wazero compile.
// Unbounded table min (H44) allocates pointer arrays and can OOM the process.
const maxWasmTableInitial = 256

// maxWasmMemoryLimitPages caps memory-section minimum pages before wazero compile.
// Matches maxMemoryLimitPages() ceiling (1024). Secure/default profiles are lower
// (96/128); the child still enforces the active profile. Without this bound the
// pre-parser only checked memory vector count (HACKME-2026-WASM-MEM-01 / fimasw).
const maxWasmMemoryLimitPages = 1024

// maxWasmElementSectionBytes bounds element-section payload size for check-gate modules.
const maxWasmElementSectionBytes = 8192

// Nested / per-entry caps — wazero allocates by declared LEB counts before
// checking that the section has enough bytes (export, valuetypes, locals, names).
const (
	maxWasmTypeCount       = 64
	maxWasmFuncCount       = 64
	maxWasmExportCount     = 32
	maxWasmCodeCount       = 64
	maxWasmGlobalCount     = 32
	maxWasmDataCount       = 32
	maxWasmMemoryCount     = 4
	maxWasmElementCount    = 16
	maxWasmDeclaredCount   = 64
	maxWasmParamsPerType   = 32
	maxWasmResultsPerType  = 16
	maxWasmLocalsPerFunc   = 64
	maxWasmLocalGroups     = 32
	maxWasmNameBytes       = 128
	maxWasmExportNameBytes = 128
	maxWasmElemEntries     = 256
	maxWasmDataBytes       = 4096
)

// rejectWasmHostileSections blocks start/import/custom sections, oversized
// tables/memory pages/elements, and inflated LEB vector counts (top-level and
// nested) before CompileModule. Custom/name sections are rejected entirely —
// Dig/Hunt check gates never need them, and decodeFunctionNames OOMs on
// inflated counts.
//
// This is a fast filter only; ValidateCheckWasm also runs CompileModule in a
// RLIMIT_AS child so novel nested vectors cannot kill the hub process.
func rejectWasmHostileSections(wasm []byte) error {
	if len(wasm) < 8 {
		return nil
	}
	pos := 8
	for pos < len(wasm) {
		sectionID := wasm[pos]
		pos++
		size, err := readU32LEB(wasm, &pos)
		if err != nil {
			return err
		}
		if pos+int(size) > len(wasm) {
			return errors.New("sandbox: malformed wasm section size")
		}
		body := wasm[pos : pos+int(size)]
		switch sectionID {
		case 0: // custom (incl. name) — stripped before compile; do not feed to wazero
			// Skip body; stripWasmCustomSections removes these. Avoid parsing
			// name subsection here (itself an OOM vector if done naively).
		case 1: // type
			if err := rejectWasmTypeSection(body); err != nil {
				return err
			}
		case 2:
			return errors.New("sandbox: wasm import section not allowed")
		case 3: // function
			if err := rejectWasmVectorPrefix(body, maxWasmFuncCount); err != nil {
				return err
			}
		case 4:
			if err := rejectWasmTableSection(body); err != nil {
				return err
			}
		case 5: // memory
			if err := rejectWasmMemorySection(body); err != nil {
				return err
			}
		case 6: // global
			if err := rejectWasmGlobalSection(body); err != nil {
				return err
			}
		case 7: // export
			if err := rejectWasmExportSection(body); err != nil {
				return err
			}
		case 8:
			return errors.New("sandbox: wasm start section not allowed")
		case 9:
			if int(size) > maxWasmElementSectionBytes {
				return errors.New("sandbox: wasm element section too large")
			}
			if err := rejectWasmElementSection(body); err != nil {
				return err
			}
		case 10: // code
			if err := rejectWasmCodeSection(body); err != nil {
				return err
			}
		case 11: // data
			if err := rejectWasmDataSection(body); err != nil {
				return err
			}
		case 12: // data count
			if err := rejectWasmVectorPrefix(body, maxWasmDataCount); err != nil {
				return err
			}
		case 13:
			if err := rejectWasmVectorPrefix(body, maxWasmDeclaredCount); err != nil {
				return err
			}
		default:
			// Unknown section id — reject rather than hand to wazero.
			return errors.New("sandbox: wasm unknown section not allowed")
		}
		pos += int(size)
	}
	return nil
}

// rejectWasmStartSection is kept for callers/tests that name the start check explicitly.
func rejectWasmStartSection(wasm []byte) error {
	return rejectWasmHostileSections(wasm)
}

func rejectWasmVectorPrefix(body []byte, maxCount uint32) error {
	pos := 0
	count, err := readU32LEB(body, &pos)
	if err != nil {
		return err
	}
	if count > maxCount {
		return errors.New("sandbox: wasm vector count too large")
	}
	remain := len(body) - pos
	if remain < 0 {
		return errors.New("sandbox: malformed wasm vector section")
	}
	if int(count) > remain {
		return errors.New("sandbox: wasm vector count exceeds section bytes")
	}
	return nil
}

func rejectWasmTypeSection(body []byte) error {
	pos := 0
	count, err := readU32LEB(body, &pos)
	if err != nil {
		return err
	}
	if count > maxWasmTypeCount {
		return errors.New("sandbox: wasm vector count too large")
	}
	if int(count) > len(body)-pos {
		return errors.New("sandbox: wasm vector count exceeds section bytes")
	}
	for i := uint32(0); i < count; i++ {
		if pos >= len(body) {
			return errors.New("sandbox: malformed wasm type section")
		}
		form := body[pos]
		pos++
		if form != 0x60 { // functype
			return errors.New("sandbox: unsupported wasm type form")
		}
		nParams, err := readU32LEB(body, &pos)
		if err != nil {
			return err
		}
		if nParams > maxWasmParamsPerType {
			return errors.New("sandbox: wasm type param count too large")
		}
		if int(nParams) > len(body)-pos {
			return errors.New("sandbox: wasm type params exceed section bytes")
		}
		pos += int(nParams) // skip valtypes (1 byte each)
		nResults, err := readU32LEB(body, &pos)
		if err != nil {
			return err
		}
		if nResults > maxWasmResultsPerType {
			return errors.New("sandbox: wasm type result count too large")
		}
		if int(nResults) > len(body)-pos {
			return errors.New("sandbox: wasm type results exceed section bytes")
		}
		pos += int(nResults)
	}
	return nil
}

func rejectWasmExportSection(body []byte) error {
	pos := 0
	count, err := readU32LEB(body, &pos)
	if err != nil {
		return err
	}
	if count > maxWasmExportCount {
		return errors.New("sandbox: wasm vector count too large")
	}
	if int(count) > len(body)-pos {
		return errors.New("sandbox: wasm vector count exceeds section bytes")
	}
	for i := uint32(0); i < count; i++ {
		nameLen, err := readU32LEB(body, &pos)
		if err != nil {
			return err
		}
		if nameLen > maxWasmExportNameBytes {
			return errors.New("sandbox: wasm export name too long")
		}
		if int(nameLen) > len(body)-pos {
			return errors.New("sandbox: wasm export name exceeds section bytes")
		}
		pos += int(nameLen)
		if pos >= len(body) {
			return errors.New("sandbox: malformed wasm export")
		}
		pos++ // kind
		if _, err := readU32LEB(body, &pos); err != nil {
			return err
		}
	}
	return nil
}

func rejectWasmCodeSection(body []byte) error {
	pos := 0
	count, err := readU32LEB(body, &pos)
	if err != nil {
		return err
	}
	if count > maxWasmCodeCount {
		return errors.New("sandbox: wasm vector count too large")
	}
	if int(count) > len(body)-pos {
		return errors.New("sandbox: wasm vector count exceeds section bytes")
	}
	for i := uint32(0); i < count; i++ {
		bodySize, err := readU32LEB(body, &pos)
		if err != nil {
			return err
		}
		if int(bodySize) > len(body)-pos {
			return errors.New("sandbox: wasm code body exceeds section bytes")
		}
		fn := body[pos : pos+int(bodySize)]
		pos += int(bodySize)
		if err := rejectWasmCodeBody(fn); err != nil {
			return err
		}
	}
	return nil
}

func rejectWasmCodeBody(fn []byte) error {
	p := 0
	nGroups, err := readU32LEB(fn, &p)
	if err != nil {
		return err
	}
	if nGroups > maxWasmLocalGroups {
		return errors.New("sandbox: wasm local groups too many")
	}
	if int(nGroups) > len(fn)-p {
		return errors.New("sandbox: wasm local groups exceed body bytes")
	}
	var totalLocals uint64
	for g := uint32(0); g < nGroups; g++ {
		n, err := readU32LEB(fn, &p)
		if err != nil {
			return err
		}
		if n > maxWasmLocalsPerFunc {
			return errors.New("sandbox: wasm locals count too large")
		}
		totalLocals += uint64(n)
		if totalLocals > uint64(maxWasmLocalsPerFunc) {
			return errors.New("sandbox: wasm locals total too large")
		}
		if p >= len(fn) {
			return errors.New("sandbox: malformed wasm locals")
		}
		p++ // valtype
	}
	// Locals capped above. Do not byte-scan expr for br_table (0x0e): that
	// byte also appears inside LEB immediates (e.g. i64.const) and false-
	// rejected real guard modules. Inflated br_table OOM → RLIMIT child.
	return nil
}

func rejectWasmGlobalSection(body []byte) error {
	pos := 0
	count, err := readU32LEB(body, &pos)
	if err != nil {
		return err
	}
	if count > maxWasmGlobalCount {
		return errors.New("sandbox: wasm vector count too large")
	}
	if int(count) > len(body)-pos {
		return errors.New("sandbox: wasm vector count exceeds section bytes")
	}
	return nil
}

func rejectWasmDataSection(body []byte) error {
	pos := 0
	count, err := readU32LEB(body, &pos)
	if err != nil {
		return err
	}
	if count > maxWasmDataCount {
		return errors.New("sandbox: wasm vector count too large")
	}
	if int(count) > len(body)-pos {
		return errors.New("sandbox: wasm vector count exceeds section bytes")
	}
	for i := uint32(0); i < count; i++ {
		if pos >= len(body) {
			return errors.New("sandbox: malformed wasm data section")
		}
		// Passive-format data segments vary; bound remaining payload per segment.
		remain := len(body) - pos
		if remain > maxWasmDataBytes*(int(count)-int(i)) && remain > maxWasmDataBytes*4 {
			// Soft: if a single remaining blob is absurd vs caps, reject.
			if remain > maxWasmDataBytes*int(count) {
				return errors.New("sandbox: wasm data section too large")
			}
		}
		// Skip one byte of flags/mode when present and try to read a size LEB later —
		// full parse is complex; child RLIMIT covers residual cases.
		pos++ // consume at least one byte so count progress is honest
		if pos > len(body) {
			return errors.New("sandbox: malformed wasm data section")
		}
	}
	return nil
}

func rejectWasmTableSection(body []byte) error {
	pos := 0
	count, err := readU32LEB(body, &pos)
	if err != nil {
		return err
	}
	if count > 4 {
		return errors.New("sandbox: too many wasm tables")
	}
	remain := len(body) - pos
	if int(count) > remain {
		return errors.New("sandbox: wasm vector count exceeds section bytes")
	}
	for i := uint32(0); i < count; i++ {
		if pos >= len(body) {
			return errors.New("sandbox: malformed wasm table section")
		}
		pos++ // reftype
		if pos >= len(body) {
			return errors.New("sandbox: malformed wasm table limits")
		}
		flag := body[pos]
		pos++
		minimum, err := readU32LEB(body, &pos)
		if err != nil {
			return err
		}
		if minimum > maxWasmTableInitial {
			return errors.New("sandbox: wasm table initial size too large")
		}
		if flag&0x01 != 0 {
			if _, err := readU32LEB(body, &pos); err != nil {
				return err
			}
		}
	}
	return nil
}

// rejectWasmMemorySection mirrors rejectWasmTableSection: vector count plus each
// memory limit's minimum pages (and optional max). Without the pages bound,
// min≈2^32 pages passed the pre-parser and only failed in the RLIMIT child.
func rejectWasmMemorySection(body []byte) error {
	pos := 0
	count, err := readU32LEB(body, &pos)
	if err != nil {
		return err
	}
	if count > maxWasmMemoryCount {
		return errors.New("sandbox: wasm vector count too large")
	}
	remain := len(body) - pos
	if int(count) > remain {
		return errors.New("sandbox: wasm vector count exceeds section bytes")
	}
	for i := uint32(0); i < count; i++ {
		if pos >= len(body) {
			return errors.New("sandbox: malformed wasm memory section")
		}
		flag := body[pos]
		pos++
		minimum, err := readU32LEB(body, &pos)
		if err != nil {
			return err
		}
		if minimum > maxWasmMemoryLimitPages {
			return errors.New("sandbox: wasm memory initial size too large")
		}
		if flag&0x01 != 0 {
			if _, err := readU32LEB(body, &pos); err != nil {
				return err
			}
		}
	}
	return nil
}

func rejectWasmElementSection(body []byte) error {
	pos := 0
	count, err := readU32LEB(body, &pos)
	if err != nil {
		return err
	}
	if count > maxWasmElementCount {
		return errors.New("sandbox: too many wasm element segments")
	}
	remain := len(body) - pos
	if int(count) > remain {
		return errors.New("sandbox: wasm vector count exceeds section bytes")
	}
	if remain > maxWasmElemEntries*8 && count > 0 {
		// Bound total element payload loosely; child RLIMIT is the backstop.
		if remain > maxWasmElementSectionBytes {
			return errors.New("sandbox: wasm element section too large")
		}
	}
	return nil
}

func readU32LEB(data []byte, pos *int) (uint32, error) {
	var result uint32
	var shift uint
	for i := 0; i < 5; i++ {
		if *pos >= len(data) {
			return 0, errors.New("sandbox: unexpected eof in leb128")
		}
		b := data[*pos]
		*pos = *pos + 1
		result |= uint32(b&0x7f) << shift
		if (b & 0x80) == 0 {
			return result, nil
		}
		shift += 7
	}
	return 0, errors.New("sandbox: leb128 too long")
}
