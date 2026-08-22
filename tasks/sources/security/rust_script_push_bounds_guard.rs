//! Bitcoin script push-size bound guard — u64 mode with wasm edge bitmap (Phase 2 P2a).
#![no_std]

use core::panic::PanicInfo;

const COV_MEM_OFF: usize = 4096;
const COV_MEM_LEN: usize = 256;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    loop {}
}

/// Bitcoin Core-inspired: script.h MAX_SCRIPT_ELEMENT_SIZE (520), script.cpp GetScriptOp push parse.
/// Input packs: opcode in low byte, claimed push length in bits 8..23 (see check()).
/// Returns 1 when the tuple violates the "max push element 520 bytes" style bound.
#[no_mangle]
pub extern "C" fn check(n: i64) -> i32 {
    cov_reset();
    if n <= 0 {
        cov_hit(1);
        return 0;
    }
    let op = (n & 0xff) as u32;
    let claimed_len = ((n >> 8) & 0xffff) as u32;
    // OP_PUSHDATA1 (0x4c) with oversized push — consensus-class violation (simplified).
    if op == 0x4c && claimed_len > 520 {
        cov_hit(10);
        1
    } else {
        if op == 0x4c {
            cov_hit(11);
        } else {
            cov_hit(12);
        }
        cov_hit(3);
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
