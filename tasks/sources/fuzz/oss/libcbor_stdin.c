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
	struct cbor_load_result result;
	cbor_item_t *item = cbor_load(buf, n, &result);
	if (item) {
		cbor_decref(&item);
	}
	return 0;
}
