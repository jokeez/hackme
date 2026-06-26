#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "heatshrink_decoder.h"

int main(void) {
	static uint8_t inbuf[65537];
	static uint8_t outbuf[65536];
	size_t n = fread(inbuf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	/* HEATSHRINK_DYNAMIC_ALLOC=1: stack struct is too small (flexible buffers[]). */
	heatshrink_decoder *hsd = heatshrink_decoder_alloc(256, 8, 4);
	if (hsd == NULL) {
		return 0;
	}
	size_t in_sz = n;
	size_t out_sz = sizeof(outbuf);
	size_t sunk = 0;
	(void)heatshrink_decoder_sink(hsd, inbuf, in_sz, &sunk);
	out_sz = sizeof(outbuf);
	size_t polled = 0;
	(void)heatshrink_decoder_poll(hsd, outbuf, out_sz, &polled);
	(void)heatshrink_decoder_finish(hsd);
	heatshrink_decoder_free(hsd);
	return 0;
}
