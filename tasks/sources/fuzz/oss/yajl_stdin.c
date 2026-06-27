#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <yajl/yajl_parse.h>
#include <yajl/yajl_tree.h>

static int noop_null(void *ctx) { (void)ctx; return 1; }
static int noop_bool(void *ctx, int v) { (void)ctx; (void)v; return 1; }
static int noop_number(void *ctx, const char *s, size_t len) { (void)ctx; (void)s; (void)len; return 1; }
static int noop_string(void *ctx, const unsigned char *s, size_t len) { (void)ctx; (void)s; (void)len; return 1; }
static int noop_start_map(void *ctx) { (void)ctx; return 1; }
static int noop_map_key(void *ctx, const unsigned char *s, size_t len) { (void)ctx; (void)s; (void)len; return 1; }
static int noop_end_map(void *ctx) { (void)ctx; return 1; }
static int noop_start_array(void *ctx) { (void)ctx; return 1; }
static int noop_end_array(void *ctx) { (void)ctx; return 1; }

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	static const yajl_callbacks cbs = {
		noop_null, noop_bool, NULL, NULL, noop_number, noop_string,
		noop_start_map, noop_map_key, noop_end_map,
		noop_start_array, noop_end_array,
	};
	yajl_handle h = yajl_alloc(&cbs, NULL, NULL);
	if (!h) {
		return 0;
	}
	(void)yajl_parse(h, (const unsigned char *)buf, n);
	(void)yajl_complete_parse(h);
	yajl_free(h);

	/* DOM path — CVE-2023-33460 class (yajl_tree_parse) */
	char errbuf[256];
	yajl_val tree = yajl_tree_parse(buf, errbuf, sizeof(errbuf));
	if (tree) {
		yajl_tree_free(tree);
	}
	return 0;
}
