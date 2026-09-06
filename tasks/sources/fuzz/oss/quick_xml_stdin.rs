//! Hunt Rust — ASAN stdin driver for quick-xml (parser with unsafe internals).
//! Feeds raw bytes through the event reader until EOF or error.

use std::io::{self, Read};

use quick_xml::events::Event;
use quick_xml::reader::Reader;

fn main() {
	let mut input = Vec::new();
	let _ = io::stdin().take(65536).read_to_end(&mut input);
	if input.is_empty() {
		return;
	}
	let mut reader = Reader::from_reader(input.as_slice());
	reader.config_mut().check_end_names = false;
	reader.config_mut().trim_text(true);
	let mut buf = Vec::new();
	loop {
		match reader.read_event_into(&mut buf) {
			Ok(Event::Eof) => break,
			Err(_) => break,
			_ => buf.clear(),
		}
	}
}
