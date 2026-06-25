#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "cJSON.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	buf[n] = '\0';
	cJSON *j = cJSON_Parse(buf);
	if (j) {
		cJSON_Delete(j);
	}
	return 0;
}
