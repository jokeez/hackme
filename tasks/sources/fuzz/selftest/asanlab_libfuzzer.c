#include <stdint.h>
#include <stddef.h>

#include "asanlab.h"

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
	if (size > 65536) {
		size = 65536;
	}
	asanlab_parse(data, size);
	return 0;
}
