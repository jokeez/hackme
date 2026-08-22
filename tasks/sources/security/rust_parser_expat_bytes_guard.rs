//! Parser expat portable verify — byte mode (check_bytes).
//! WASM flags obvious malformed XML; native ASAN expat (oss_upstream repro) confirms crashes.
#![no_std]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    loop {}
}

const MAX_INPUT: usize = 4096;

#[no_mangle]
pub extern "C" fn check_bytes(ptr: *const u8, len: i32) -> i32 {
    if len <= 0 {
        return 0;
    }
    let len = len as usize;
    if len > MAX_INPUT {
        return 0;
    }
    let data = unsafe { core::slice::from_raw_parts(ptr, len) };
    if detect_malformed_xml(data) {
        1
    } else {
        0
    }
}

fn detect_malformed_xml(b: &[u8]) -> bool {
    if b.is_empty() {
        return false;
    }
    let mut lt = 0usize;
    let mut gt = 0usize;
    for &c in b {
        match c {
            b'<' => lt += 1,
            b'>' => gt += 1,
            0 => return true,
            _ => {}
        }
    }
    if lt != gt {
        return true;
    }
    if contains(b, b"<<") || contains(b, b"<>") {
        return true;
    }
    if let Some(i) = find_byte(b, b'&') {
        let rest = &b[i + 1..];
        if rest.is_empty() || !rest.iter().take(12).any(|&c| c == b';') {
            return true;
        }
    }
    if b.starts_with(b"<") && !b.contains(&b'>') {
        return true;
    }
    false
}

fn find_byte(hay: &[u8], needle: u8) -> Option<usize> {
    hay.iter().position(|&c| c == needle)
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
