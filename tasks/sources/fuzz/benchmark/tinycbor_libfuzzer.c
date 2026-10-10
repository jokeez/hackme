#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include "cbor.h"

int LLVMFuzzerTestOneInput(const uint8_t *data, size_t size) {
	if (size == 0 || size > 65536) {
		return 0;
	}
	CborParser parser;
	CborValue it;
	if (cbor_parser_init(data, size, 0, &parser, &it) != CborNoError) {
		return 0;
	}
	for (unsigned steps = 0; steps < 10000 && !cbor_value_at_end(&it); steps++) {
		if (cbor_value_advance(&it) != CborNoError) {
			break;
		}
	}
	return 0;
}
