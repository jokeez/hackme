package fuzz

// Pre-built Wasm 1.0 modules (wat2wasm) for adversarial sandbox tests.
// All export `check(i64)->i32` unless noted — the pool gate surface.

const (
	// infiniteLoopCheckWasmHex — busy-loop in check(); must hit InvokeCheck/validate timeout.
	infiniteLoopCheckWasmHex = "0061736d0100000001060160017e017f0302010007090105636865636b00000a0b01090003400c000b41010b"

	// oobLoadCheckWasmHex — i32.load at offset 65536 (past 64KiB page); must trap.
	oobLoadCheckWasmHex = "0061736d0100000001060160017e017f03020100050301000107090105636865636b00000a0e010c00418080042802001a41010b"

	// wasiImportCheckWasmHex — imports wasi fd_write; must fail export policy before any host I/O.
	wasiImportCheckWasmHex = "0061736d01000000010e0260047f7f7f7f017f60017e017f02230116776173695f736e617073686f745f70726576696577310866645f777269746500000302010107090105636865636b00010a0601040041010b"

	// evalInfiniteLoopWasmHex — exports eval(i64)->i64 with infinite loop (PoH lock path in sandbox.Eval).
	evalInfiniteLoopWasmHex = "0061736d0100000001060160017e017e03020100070801046576616c00000a0b01090003400c000b20000b"
)
