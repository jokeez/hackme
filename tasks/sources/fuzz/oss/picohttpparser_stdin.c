#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "picohttpparser.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	const char *method = NULL, *path = NULL;
	size_t method_len = 0, path_len = 0;
	int minor = 0;
	struct phr_header headers[32];
	size_t num_headers = 32;
	(void)phr_parse_request(buf, n, &method, &method_len, &path, &path_len, &minor, headers, &num_headers, 0);

	num_headers = 32;
	int status = 0;
	const char *msg = NULL;
	size_t msg_len = 0;
	(void)phr_parse_response(buf, n, &minor, &status, &msg, &msg_len, headers, &num_headers, 0);

	num_headers = 32;
	(void)phr_parse_headers(buf, n, headers, &num_headers, 0);
	return 0;
}
