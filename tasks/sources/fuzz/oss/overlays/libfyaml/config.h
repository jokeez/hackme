/* HackMe OSS CVE fuzz — minimal libfyaml config.h for bare clang build. */
#ifndef CONFIG_H
#define CONFIG_H
#define HAVE___BUILTIN_BSWAP16 1
#define HAVE___BUILTIN_BSWAP32 1
#define HAVE___BUILTIN_BSWAP64 1
#define HAVE_QSORT_R 1
#define HAVE_MREMAP 1
#define HAVE_DECL_ENVIRON 1
#define TARGET_HAS_SSE2 0
#define TARGET_HAS_SSE41 0
#define TARGET_HAS_AVX2 0
#endif
