#ifndef JSON_C_FUZZ_JSON_CONFIG_H
#define JSON_C_FUZZ_JSON_CONFIG_H
#include <stdint.h>
#include <inttypes.h>
#define JSON_C_HAVE_INTTYPES_H 1
#define JSON_C_HAVE_STDINT_H 1
typedef int64_t JSON_INT64;
typedef uint64_t JSON_UINT64;
#define JSON_INT64_MAX INT64_MAX
#define JSON_INT64_MIN INT64_MIN
#define JSON_UINT64_MAX UINT64_MAX
#endif
