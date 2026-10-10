#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "cbor.h"

/* Deep v1: load → serialize → reload roundtrip. */
int main(void) {
	static uint8_t buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}

	struct cbor_load_result result;
	cbor_item_t *item = cbor_load(buf, n, &result);
	if (!item) {
		return 0;
	}

	unsigned char *serialized = NULL;
	size_t serialize_len = 0;
	cbor_serialize_alloc(item, &serialized, &serialize_len);
	if (serialized && serialize_len > 0 && serialize_len < 65536) {
		struct cbor_load_result r2;
		cbor_item_t *again = cbor_load(serialized, serialize_len, &r2);
		if (again) {
			cbor_decref(&again);
		}
		free(serialized);
	} else if (serialized) {
		free(serialized);
	}

	cbor_decref(&item);
	return 0;
}
