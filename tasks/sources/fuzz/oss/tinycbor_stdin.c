#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "cbor.h"

int main(void) {
	static uint8_t buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	CborParser parser;
	CborValue it;
	if (cbor_parser_init(buf, n, 0, &parser, &it) != CborNoError) {
		return 0;
	}
	for (unsigned steps = 0; steps < 10000 && !cbor_value_at_end(&it); steps++) {
		if (cbor_value_advance(&it) != CborNoError) {
			break;
		}
	}
	return 0;
}
