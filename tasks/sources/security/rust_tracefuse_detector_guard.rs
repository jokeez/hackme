//! Tracefuse-inspired supply-chain detector guard for HackMe pool fuzzing.
//! Ports key byte patterns from https://github.com/FounderB/Tracefuse
//! (secrets + dockerfile + npm lifecycle heuristics) into check(i64).
#![no_std]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    loop {}
}

#[no_mangle]
pub extern "C" fn check(n: i64) -> i32 {
    if n <= 0 {
        return 0;
    }
    let mut b = [0u8; 8];
    let mut v = n as u64;
    for byte in b.iter_mut() {
        *byte = (v & 0xff) as u8;
        v >>= 8;
    }
    if detect_tracefuse(&b) {
        1
    } else {
        0
    }
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

fn detect_tracefuse(b: &[u8]) -> bool {
    // secrets detector (Tracefuse detect/secrets.rs)
    if contains(b, b"AKIA") || contains(b, b"ASIA") {
        return true;
    }
    if contains(b, b"ghp_") || contains(b, b"github_pat_") {
        return true;
    }
    if contains(b, b"sk_live_") || contains(b, b"sk-proj-") {
        return true;
    }
    if contains(b, b"xoxb-") || contains(b, b"xoxp-") {
        return true;
    }
    if contains(b, b"AIza") {
        return true;
    }
    if contains(b, b"-----BEG") || contains(b, b"PRIVATE") {
        return true;
    }
    if contains(b, b"hooks.slack") {
        return true;
    }
    // dockerfile detector (Tracefuse detect/dockerfile.rs)
    if contains(b, b":latest") || contains(b, b":lat") {
        return true;
    }
    if contains(b, b"ENV") && (contains(b, b"SECRET") || contains(b, b"PASSWORD") || contains(b, b"TOKEN")) {
        return true;
    }
    if contains(b, b"curl") && contains(b, b"| sh") {
        return true;
    }
    if contains(b, b"ADD http") {
        return true;
    }
    // npm scripts (Tracefuse detect/scripts.rs)
    if contains(b, b"postinst") || contains(b, b"preinsta") {
        return true;
    }
    if contains(b, b"eval(") || contains(b, b"| bash") {
        return true;
    }
    // ci (Tracefuse detect/ci.rs) — partial 8-byte window
    if contains(b, b"pull_req") || contains(b, b"curl -s") {
        return true;
    }
    false
}
