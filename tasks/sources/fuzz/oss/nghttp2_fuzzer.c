/* libFuzzer harness — nghttp2 client session mem_recv (same surface as nghttp2_stdin.c). */
#define _GNU_SOURCE
#include <nghttp2/nghttp2.h>
#include <stddef.h>
#include <stdint.h>

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
	nghttp2_session *session = NULL;
	nghttp2_session_callbacks *callbacks = NULL;

	if (size == 0 || size > 65536) {
		return 0;
	}
	if (nghttp2_session_callbacks_new(&callbacks) != 0) {
		return 0;
	}
	if (nghttp2_session_client_new(&session, callbacks, NULL) != 0) {
		nghttp2_session_callbacks_del(callbacks);
		return 0;
	}
	(void)nghttp2_session_mem_recv(session, data, size);
	nghttp2_session_del(session);
	nghttp2_session_callbacks_del(callbacks);
	return 0;
}
