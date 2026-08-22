//! Customer bytes guard template for HackMe pool (check_bytes + wasm edge bitmap).
//! Copy this file, implement `detect_violation`, build to WASM, upload as wasm_check_hex.
//!
//! Build:
//!   rustc --target wasm32-unknown-unknown -O --crate-type=cdylib \
//!     rust_customer_bytes_guard_template.rs \
//!     -o my_guard.wasm
//!
//! Campaign config (wizard / security-audit):
//!   input_mode: "bytes"
//!   max_input_bytes: 1024   // std tier (4096 = pro)
//!   guided_scheduling: true
//!   coverage_kind: "wasm_edge_bitmap"   // honest guided scheduling on detector branches
//!   seed_byte_corpus: ["hex-encoded seeds..."]
//!
//! Coverage contract: call cov_hit(id) on each detector branch; sandbox reads bytes
//! [8192..8448) from exported linear memory after every check_bytes invocation.
#![no_std]

use core::panic::PanicInfo;

const COV_MEM_OFF: usize = 8192;
const COV_MEM_LEN: usize = 256;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    loop {}
}

/// Platform max — must match sandbox.MaxCheckInputBytes() (4096).
const MAX_INPUT: usize = 4096;

#[no_mangle]
pub extern "C" fn check_bytes(ptr: *const u8, len: i32) -> i32 {
    cov_reset();
    if len <= 0 {
        cov_hit(1);
        return 0;
    }
    let len = len as usize;
    if len > MAX_INPUT {
        cov_hit(2);
        return 0;
    }
    let data = unsafe { core::slice::from_raw_parts(ptr, len) };
    if detect_violation(data) {
        1
    } else {
        0
    }
}

fn cov_reset() {
    unsafe {
        let cov = core::slice::from_raw_parts_mut(COV_MEM_OFF as *mut u8, COV_MEM_LEN);
        for b in cov.iter_mut() {
            *b = 0;
        }
    }
}

fn cov_hit(id: u8) {
    let idx = (id as usize) % COV_MEM_LEN;
    unsafe {
        let p = COV_MEM_OFF as *mut u8;
        let v = core::ptr::read(p.add(idx));
        core::ptr::write(p.add(idx), v.saturating_add(1));
    }
}

/// Return true when input violates your property / detector rule.
fn detect_violation(input: &[u8]) -> bool {
    // Example 1: reject oversized push (Bitcoin script-style layout in bytes).
    if input.len() >= 3 && input[0] == 0x4c {
        let claimed = u16::from_le_bytes([input[1], input[2]]) as u32;
        if claimed > 520 {
            cov_hit(10);
            return true;
        }
    }
    // Example 2: secret prefix detector (supply-chain).
    if contains(input, b"AKIA") || contains(input, b"ghp_") {
        cov_hit(11);
        return true;
    }
    // Example 3: malformed filter expr class (invalid UTF-8 + bare '=').
    if input == b"\xc7=" {
        cov_hit(12);
        return true;
    }
    cov_hit(3);
    false
}

fn contains(hay: &[u8], needle: &[u8]) -> bool {
    if needle.is_empty() || needle.len() > hay.len() {
        return false;
    }
    let last = hay.len() - needle.len();
    for i in 0..=last {
        if &hay[i..i + needle.len()] == needle {
            return true;
        }
    }
    false
}
