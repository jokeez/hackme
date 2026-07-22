package sandbox

import "errors"

// maxWasmTableInitial caps funcref/externref table minimum size before wazero compile.
// Unbounded table min (H44) allocates pointer arrays and can OOM the process.
const maxWasmTableInitial = 256

// maxWasmElementSectionBytes bounds element-section payload size for check-gate modules.
const maxWasmElementSectionBytes = 8192

// rejectWasmHostileSections blocks start routines and oversized table/element sections
// before CompileModule (DoS via infinite start or huge funcref tables).
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
		if int(size) < 0 || pos+int(size) > len(wasm) {
			return errors.New("sandbox: malformed wasm section size")
		}
		body := wasm[pos : pos+int(size)]
		switch sectionID {
		case 8:
			return errors.New("sandbox: wasm start section not allowed")
		case 4:
			if err := rejectWasmTableSection(body); err != nil {
				return err
			}
		case 9:
			if int(size) > maxWasmElementSectionBytes {
				return errors.New("sandbox: wasm element section too large")
			}
			if err := rejectWasmElementSection(body); err != nil {
				return err
			}
		}
		pos += int(size)
	}
	return nil
}

// rejectWasmStartSection is kept for callers/tests that name the start check explicitly.
func rejectWasmStartSection(wasm []byte) error {
	return rejectWasmHostileSections(wasm)
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
	for i := uint32(0); i < count; i++ {
		if pos >= len(body) {
			return errors.New("sandbox: malformed wasm table section")
		}
		// reftype (funcref 0x70 / externref 0x6f)
		pos++
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

func rejectWasmElementSection(body []byte) error {
	pos := 0
	count, err := readU32LEB(body, &pos)
	if err != nil {
		return err
	}
	// Active element segments can materialize large funcref tables; keep count tiny for gates.
	if count > 16 {
		return errors.New("sandbox: too many wasm element segments")
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
