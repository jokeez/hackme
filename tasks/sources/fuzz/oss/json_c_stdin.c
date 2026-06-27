#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "json.h"

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	buf[n] = '\0';
	struct json_object *obj = json_tokener_parse(buf);
	if (obj) {
		json_object_put(obj);
	}
	return 0;
}
