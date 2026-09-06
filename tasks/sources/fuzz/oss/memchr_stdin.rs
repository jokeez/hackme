//! Hunt Rust — ASAN stdin driver for memchr (unsafe SIMD / memmem).
//! Exercises byte-search APIs that sit on top of unsafe implementations.

use std::io::{self, Read};

fn main() {
	let mut buf = Vec::new();
	let _ = io::stdin().take(65536).read_to_end(&mut buf);
	if buf.is_empty() {
		return;
	}
	// First byte = needle; remainder = haystack (empty haystack is OK).
	let needle = buf[0];
	let hay = &buf[1..];
	let _ = memchr::memchr(needle, hay);
	let _ = memchr::memrchr(needle, hay);
	let _ = memchr::memchr2(needle, needle ^ 1, hay);
	let _ = memchr::memchr3(needle, needle ^ 1, needle ^ 2, hay);
	let _ = memchr::memmem::find(hay, &[needle]);
	if hay.len() >= 2 {
		let _ = memchr::memmem::find(hay, &hay[..2.min(hay.len())]);
	}
}
