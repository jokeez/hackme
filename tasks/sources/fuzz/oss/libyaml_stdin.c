#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <yaml.h>

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	yaml_parser_t parser;
	yaml_event_t event;
	if (!yaml_parser_initialize(&parser)) {
		return 0;
	}
	yaml_parser_set_input_string(&parser, (const unsigned char *)buf, n);
	while (yaml_parser_parse(&parser, &event)) {
		yaml_event_delete(&event);
	}
	yaml_parser_delete(&parser);
	return 0;
}
