#![no_std]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    loop {}
}

#[no_mangle]
pub extern "C" fn check(n: i64) -> i32 {
    // Synthetic state-transition style gate:
    // derive "from->to" states and allow only one transition family.
    let u = n as u64;
    let from = (u >> 3) & 0x7;
    let to = (u >> 9) & 0x7;
    let token = u & 0xFF;
    if from == 1 && to == 4 && token == 0xA5 {
        1
    } else {
        0
    }
}
