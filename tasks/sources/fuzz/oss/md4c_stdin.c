#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "md4c.h"

static int md_cb_enter_block(MD_BLOCKTYPE t, void *d, void *u) {
	(void)t;
	(void)d;
	(void)u;
	return 0;
}
static int md_cb_leave_block(MD_BLOCKTYPE t, void *d, void *u) {
	(void)t;
	(void)d;
	(void)u;
	return 0;
}
static int md_cb_enter_span(MD_SPANTYPE t, void *d, void *u) {
	(void)t;
	(void)d;
	(void)u;
	return 0;
}
static int md_cb_leave_span(MD_SPANTYPE t, void *d, void *u) {
	(void)t;
	(void)d;
	(void)u;
	return 0;
}
static int md_cb_text(MD_TEXTTYPE t, const MD_CHAR *x, MD_SIZE n, void *u) {
	(void)t;
	(void)x;
	(void)n;
	(void)u;
	return 0;
}

int main(void) {
	static unsigned char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	MD_PARSER parser = {0};
	parser.enter_block = md_cb_enter_block;
	parser.leave_block = md_cb_leave_block;
	parser.enter_span = md_cb_enter_span;
	parser.leave_span = md_cb_leave_span;
	parser.text = md_cb_text;
	(void)md_parse((const MD_CHAR *)buf, (MD_SIZE)n, &parser, NULL);
	return 0;
}
