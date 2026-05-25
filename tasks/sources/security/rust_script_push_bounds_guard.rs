#![no_std]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    loop {}
}

/// Simplified Bitcoin Script push-size rule (educational / research gate).
/// Input packs: opcode in low byte, claimed push length in bits 8..23 (see check()).
/// Returns 1 when the tuple violates the "max push element 520 bytes" style bound.
#[no_mangle]
pub extern "C" fn check(n: i64) -> i32 {
    if n <= 0 {
        return 0;
    }
    let op = (n & 0xff) as u32;
    let claimed_len = ((n >> 8) & 0xffff) as u32;
    // OP_PUSHDATA1 (0x4c) with oversized push — consensus-class violation (simplified).
    if op == 0x4c && claimed_len > 520 {
        1
    } else {
        0
    }
}
