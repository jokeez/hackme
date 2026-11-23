/* Tiny in-process driver. Not an upstream project.
 * 'A' and 'B' are different heap overflows. 'L' leaks and returns. */
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

__attribute__((noinline)) static void leaf_a(char *p) { p[24] = 'A'; }

__attribute__((noinline)) static void leaf_b(char *p) {
	memcpy(p, "0123456789abcdef0123456789abcdefXXXX", 36);
}

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
	if (data == NULL || size == 0) {
		return 0;
	}
	if (data[0] == 'L') {
		char *p = (char *)malloc(32);
		if (p) {
			p[0] = 'L';
		}
		return 0;
	}
	if (data[0] == 'A') {
		char *p = (char *)malloc(8);
		if (p) {
			leaf_a(p);
			free(p);
		}
		return 0;
	}
	if (data[0] == 'B') {
		char *p = (char *)malloc(4);
		if (p) {
			leaf_b(p);
			free(p);
		}
		return 0;
	}
	return 0;
}

#ifdef LF_DRIVER_MAIN
int main(void) {
	/* Same shape as a direct leak. Calling LLVMFuzzerTestOneInput leaves the
	 * pointer in an abandoned frame that LSan still treats as reachable. */
	char *p = (char *)malloc(32);
	p[0] = 'L';
	return p[0] == 'L' ? 0 : 1;
}
#endif
