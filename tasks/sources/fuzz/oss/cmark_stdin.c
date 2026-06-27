#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <cmark.h>

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n == 0) {
		return 0;
	}
	cmark_node *doc = cmark_parse_document(buf, n, CMARK_OPT_DEFAULT);
	if (doc) {
		cmark_node_free(doc);
	}
	char *html = cmark_markdown_to_html(buf, n, CMARK_OPT_DEFAULT);
	if (html) {
		free(html);
	}
	return 0;
}
