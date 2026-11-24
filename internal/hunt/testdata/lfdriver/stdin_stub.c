/* Tiny stdin stub. The getenv key is the cache marker; the source name is not. */
#include <stdint.h>
#include <stdlib.h>
#include <unistd.h>

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
	const char *bin = getenv("HACKME_LF_STDIN_BIN");
	unsigned char buf[8];
	if (!bin || data == NULL || size == 0) {
		return 0;
	}
	(void)data;
	if (read(0, buf, sizeof buf) < 0) {
		return 0;
	}
	return 0;
}
