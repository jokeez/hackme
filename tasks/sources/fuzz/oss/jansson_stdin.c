#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <jansson.h>

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	json_error_t error;
	json_t *root = json_loadb(buf, n, 0, &error);
	if (root) {
		json_decref(root);
	}
	return 0;
}
