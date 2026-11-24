/* In-process libFuzzer harness built from a Hunt stdin driver.
 *
 * The catalog ships one `<id>_stdin.c` per target, each `int main(void)` reading the
 * input from stdin. The last-resort wrapper (stdin_subprocess_libfuzzer.c) fork+execs
 * that binary once per input, which costs two orders of magnitude in throughput and —
 * worse — leaves libFuzzer instrumenting the wrapper instead of the target, so the
 * coverage feedback that makes a guided fuzzer guided is gone.
 *
 * This shim keeps the driver exactly as written and runs it in-process: the driver is
 * compiled with -Dmain=hackme_stdin_main, and each input is handed to it as a stdin
 * stream over the caller's buffer. No fork, no exec, no pipe, and the target's own
 * edges are the ones libFuzzer counts.
 *
 * Preconditions checked by the builder: the driver's entry is main(void) and it does
 * not call exit() (which would end the session instead of the input).
 */
#include <stdint.h>
#include <stddef.h>
#include <stdio.h>

int hackme_stdin_main(void);

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
	if (size == 0 || size > 65536) {
		return 0;
	}
	FILE *in = fmemopen((void *)data, size, "r");
	if (in == NULL) {
		return 0;
	}
	FILE *saved = stdin;
	stdin = in;
	(void)hackme_stdin_main();
	stdin = saved;
	fclose(in);
	return 0;
}
