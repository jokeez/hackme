//! FluxTap-style display filter guard — byte mode (CVE-class malformed expr detector).
//! Models the panic class from FluxTap filter.go: invalid UTF-8 + ToLower index skew
//! (e.g. "\xc7=" → slice bounds panic in evalAtom).
//! Phase 2 P2a: wasm edge bitmap at linear memory offset 8192 for guided scheduling.
#![no_std]

use core::panic::PanicInfo;

const COV_MEM_OFF: usize = 8192;
const COV_MEM_LEN: usize = 256;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    loop {}
}

const LOWER_CAP: usize = 4096;

#[no_mangle]
pub extern "C" fn check_bytes(ptr: *const u8, len: i32) -> i32 {
    cov_reset();
    if len <= 0 {
        cov_hit(1);
        return 0;
    }
    let len = len as usize;
    if len > 4096 {
        cov_hit(2);
        return 0;
    }
    let data = unsafe { core::slice::from_raw_parts(ptr, len) };
    if detect_malformed_filter(data) {
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

fn detect_malformed_filter(b: &[u8]) -> bool {
    if b == b"\xc7=" {
        cov_hit(10);
        return true;
    }
    if has_invalid_utf8_skew(b) {
        cov_hit(11);
        return true;
    }
    if is_bare_operator_expr(b) {
        cov_hit(12);
        return true;
    }
    if b.len() >= 2 && b[0] >= 0x80 {
        for i in 1..b.len() {
            if is_op_char(b[i]) && !has_field_left(b, i) {
                cov_hit(13);
                return true;
            }
        }
    }
    cov_hit(3);
    false
}

fn has_invalid_utf8_skew(b: &[u8]) -> bool {
    let (lower, lower_len) = to_lower_utf8_lossy(b);
    for op in OPS {
        if let Some(li) = find_subslice(&lower[..lower_len], op.as_bytes()) {
            if li >= b.len() {
                return true;
            }
        }
    }
    false
}

fn is_bare_operator_expr(b: &[u8]) -> bool {
    let t = trim_ascii(b);
    matches!(t, b"=" | b"==" | b"!=" | b">" | b"<" | b">=" | b"<=")
}

fn has_field_left(b: &[u8], op_at: usize) -> bool {
    let left = trim_ascii(&b[..op_at]);
    !left.is_empty() && left.iter().all(|&c| c.is_ascii_graphic() || c == b' ')
}

fn is_op_char(c: u8) -> bool {
    matches!(c, b'=' | b'!' | b'>' | b'<')
}

fn trim_ascii(b: &[u8]) -> &[u8] {
    let mut start = 0usize;
    let mut end = b.len();
    while start < end && b[start].is_ascii_whitespace() {
        start += 1;
    }
    while end > start && b[end - 1].is_ascii_whitespace() {
        end -= 1;
    }
    &b[start..end]
}

const OPS: [&str; 8] = [
    " contains ",
    " == ",
    "!=",
    ">=",
    "<=",
    ">",
    "<",
    "=",
];

fn find_subslice(hay: &[u8], needle: &[u8]) -> Option<usize> {
    if needle.is_empty() || needle.len() > hay.len() {
        return None;
    }
    let last = hay.len() - needle.len();
    for i in 0..=last {
        if &hay[i..i + needle.len()] == needle {
            return Some(i);
        }
    }
    None
}

fn to_lower_utf8_lossy(b: &[u8]) -> ([u8; LOWER_CAP], usize) {
    let mut out = [0u8; LOWER_CAP];
    let mut n = 0usize;
    let mut i = 0usize;
    while i < b.len() && n + 1 < LOWER_CAP {
        let c = b[i];
        if c < 0x80 {
            out[n] = if c >= b'A' && c <= b'Z' { c + 32 } else { c };
            n += 1;
            i += 1;
        } else {
            for &repl in b"\xEF\xBF\xBD" {
                if n >= LOWER_CAP {
                    break;
                }
                out[n] = repl;
                n += 1;
            }
            i += 1;
        }
    }
    (out, n)
}
