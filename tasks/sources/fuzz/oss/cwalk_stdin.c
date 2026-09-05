#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "cwalk.h"

int main(void) {
	static char buf[65537];
	static char out[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	buf[n] = '\0';
	(void)cwalk_path_normalize(buf, out, sizeof(out));
	(void)cwalk_path_get_absolute("/", buf, out, sizeof(out));
	return 0;
}
