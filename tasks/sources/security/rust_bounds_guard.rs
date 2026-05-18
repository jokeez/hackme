#![no_std]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    loop {}
}

#[no_mangle]
pub extern "C" fn check(n: i64) -> i32 {
    // Synthetic security gate:
    // accept only values in an expected safe window + stride constraint.
    if n >= 10_000_000 && n <= 40_000_000 && n % 97 == 0 {
        1
    } else {
        0
    }
}
