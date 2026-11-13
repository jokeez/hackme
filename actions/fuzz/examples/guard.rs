// Minimal Dig guard (WASM ABI v1): export check(i64) -> i32.
// Return 1 when the input hits the condition you are hunting for, else 0.
// Keep it #![no_std] so the module stays tiny (pool cap is 64 KiB).
#![no_std]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    loop {}
}

#[no_mangle]
pub extern "C" fn check(n: i64) -> i32 {
    // Example: flag inputs whose low 16 bits overflow a u8 length field.
    let len = (n as u64 & 0xFFFF) as u32;
    if len > u8::MAX as u32 && len.wrapping_mul(3) % 7 == 1 {
        1
    } else {
        0
    }
}
