#![no_std]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    loop {}
}

#[no_mangle]
pub extern "C" fn check(n: i64) -> i32 {
    // Synthetic overflow-safety style gate:
    // require that wrapping transform keeps upper bits clear.
    let x = (n as u64).wrapping_mul(0x9E37_79B9_7F4A_7C15);
    if (x >> 56) == 0 && (x & 0x3FF) == 0x155 {
        1
    } else {
        0
    }
}
