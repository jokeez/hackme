#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "cbor.h"

/* Deep v1: parse + validate + re-encode selected scalars/containers. */
int main(void) {
	static uint8_t buf[65537];
	static uint8_t out[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}

	CborParser parser;
	CborValue it;
	if (cbor_parser_init(buf, n, 0, &parser, &it) != CborNoError) {
		return 0;
	}
	(void)cbor_value_validate_basic(&it);

	CborEncoder encoder;
	cbor_encoder_init(&encoder, out, sizeof(out), 0);

	for (unsigned steps = 0; steps < 4096 && !cbor_value_at_end(&it); steps++) {
		CborType t = cbor_value_get_type(&it);
		switch (t) {
		case CborIntegerType: {
			int64_t v = 0;
			if (cbor_value_get_int64(&it, &v) == CborNoError) {
				(void)cbor_encode_int(&encoder, v);
			}
			break;
		}
		case CborByteStringType:
		case CborTextStringType: {
			size_t len = 0;
			if (cbor_value_calculate_string_length(&it, &len) == CborNoError && len < sizeof(out)) {
				/* length probe only — avoid large allocs in harness */
			}
			break;
		}
		case CborArrayType:
		case CborMapType:
			(void)cbor_value_is_container(&it);
			break;
		default:
			break;
		}
		if (cbor_value_advance(&it) != CborNoError) {
			break;
		}
	}
	return 0;
}
