//! Hunt Rust Phase A — ASAN stdin driver for serde_json.
//! Built via cargo +nightly with RUSTFLAGS=-Zsanitizer=address (see fuzzupstream.BuildTarget).

use std::io::{self, Read};

fn main() {
	let mut buf = Vec::new();
	let _ = io::stdin().take(65536).read_to_end(&mut buf);
	if buf.is_empty() {
		return;
	}
	let _ = serde_json::from_slice::<serde_json::Value>(&buf);
}
