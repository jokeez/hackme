//! Tracefuse supply-chain detector — byte mode (check_bytes) with wasm edge bitmap (Phase 2 P1).
#![no_std]

use core::panic::PanicInfo;

const COV_MEM_OFF: usize = 8192;
const COV_MEM_LEN: usize = 256;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    loop {}
}

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
    if detect_tracefuse(data) {
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
    if contains(b, b"AKIA") || contains(b, b"ASIA") {
        cov_hit(10);
        return true;
    }
    if contains(b, b"ghp_") || contains(b, b"github_pat_") {
        cov_hit(11);
        return true;
    }
    if contains(b, b"sk_live_") || contains(b, b"sk-proj-") {
        cov_hit(12);
        return true;
    }
    if contains(b, b"xoxb-") || contains(b, b"xoxp-") {
        cov_hit(13);
        return true;
    }
    if contains(b, b"AIza") {
        cov_hit(14);
        return true;
    }
    if contains(b, b"-----BEGIN") || contains(b, b"PRIVATE KEY") {
        cov_hit(15);
        return true;
    }
    if contains(b, b"hooks.slack") {
        cov_hit(16);
        return true;
    }
    if contains(b, b":latest") {
        cov_hit(17);
        return true;
    }
    if contains(b, b"ENV") && (contains(b, b"SECRET") || contains(b, b"PASSWORD") || contains(b, b"TOKEN")) {
        cov_hit(18);
        return true;
    }
    if contains(b, b"curl") && contains(b, b"| sh") {
        cov_hit(19);
        return true;
    }
    if contains(b, b"ADD http") {
        cov_hit(20);
        return true;
    }
    if contains(b, b"postinstall") || contains(b, b"preinstall") {
        cov_hit(21);
        return true;
    }
    if contains(b, b"eval(") || contains(b, b"| bash") {
        cov_hit(22);
        return true;
    }
    if contains(b, b"pull_request_target") || contains(b, b"curl -s") {
        cov_hit(23);
        return true;
    }
    cov_hit(3);
    false
}
