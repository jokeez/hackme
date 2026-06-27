#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <oniguruma.h>

int main(void) {
	static char buf[65537];
	size_t n = fread(buf, 1, 65536, stdin);
	if (n < 4) {
		return 0;
	}
	size_t split = n / 2;
	if (split == 0) {
		split = 1;
	}
	const UChar *pattern = (const UChar *)buf;
	const UChar *pattern_end = (const UChar *)(buf + split);
	const UChar *subject = (const UChar *)(buf + split);
	const UChar *subject_end = (const UChar *)(buf + n);

	regex_t *reg = NULL;
	OnigErrorInfo einfo;
	int r = onig_new(&reg, pattern, pattern_end, ONIG_OPTION_DEFAULT, ONIG_ENCODING_UTF8,
	                 ONIG_SYNTAX_DEFAULT, &einfo);
	if (r == ONIG_NORMAL) {
		OnigRegion *region = onig_region_new();
		if (region) {
			(void)onig_match(reg, subject, subject_end, subject, region, ONIG_OPTION_NONE);
			onig_region_free(region, 1);
		}
		onig_free(reg);
	}
	return 0;
}
