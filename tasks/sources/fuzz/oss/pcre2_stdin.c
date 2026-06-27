#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#define PCRE2_CODE_UNIT_WIDTH 8
#include <pcre2.h>

int main(void) {
	static unsigned char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n < 2) {
		return 0;
	}
	size_t split = n / 2;
	if (split == 0) {
		split = 1;
	}
	PCRE2_SPTR pattern = buf;
	PCRE2_SPTR subject = buf + split;
	PCRE2_SIZE patlen = split;
	PCRE2_SIZE sublen = n - split;
	int err = 0;
	PCRE2_SIZE erroff = 0;
	pcre2_code *re = pcre2_compile(pattern, patlen, 0, &err, &erroff, NULL);
	if (!re) {
		return 0;
	}
	pcre2_match_data *md = pcre2_match_data_create_from_pattern(re, NULL);
	if (md) {
		(void)pcre2_match(re, subject, sublen, 0, 0, md, NULL);
		pcre2_match_data_free(md);
	}
	pcre2_code_free(re);
	return 0;
}
