#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "cwalk.h"

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
	if (size == 0 || size > 65536) {
		return 0;
	}
	char *buf = (char *)malloc(size + 1);
	if (!buf) {
		return 0;
	}
	memcpy(buf, data, size);
	buf[size] = '\0';
	char out[65537];
	(void)cwk_path_normalize(buf, out, sizeof(out));
	(void)cwk_path_get_absolute("/", buf, out, sizeof(out));
	free(buf);
	return 0;
}
