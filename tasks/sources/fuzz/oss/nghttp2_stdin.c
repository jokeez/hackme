#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <nghttp2/nghttp2.h>

int main(void) {
	static unsigned char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	nghttp2_session *session = NULL;
	if (nghttp2_session_client_new(&session, NULL, NULL) != 0) {
		return 0;
	}
	(void)nghttp2_session_mem_recv(session, buf, n);
	nghttp2_session_del(session);
	return 0;
}
