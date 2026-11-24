#include <stdio.h>
#include <stdlib.h>

#include "asanlab.h"

/* stdin driver: the shape the Dig/Hunt pool runs for non-libFuzzer engines. */
int main(void) {
	static unsigned char buf[65537];
	size_t n = fread(buf, 1, sizeof(buf) - 1, stdin);
	if (n == 0) {
		return 0;
	}
	asanlab_parse(buf, n);
	return 0;
}
