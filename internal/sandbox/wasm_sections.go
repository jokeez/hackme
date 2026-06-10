package sandbox

import "errors"

// rejectWasmStartSection blocks modules that run code at instantiation (DoS via infinite start).
func rejectWasmStartSection(wasm []byte) error {
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
		if sectionID == 8 {
			return errors.New("sandbox: wasm start section not allowed")
		}
		pos += int(size)
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
