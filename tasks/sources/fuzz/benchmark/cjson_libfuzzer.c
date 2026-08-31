#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "cJSON.h"

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
	if (size > 65536) {
		size = 65536;
	}
	char *buf = (char *)malloc(size + 1);
	if (!buf) {
		return 0;
	}
	memcpy(buf, data, size);
	buf[size] = '\0';
	cJSON *j = cJSON_Parse(buf);
	if (j) {
		cJSON_Delete(j);
	}
	free(buf);
	return 0;
}
