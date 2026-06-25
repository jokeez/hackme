/*
 * HackMe OSS CVE fuzz — jansson public config (from android/jansson_config.h).
 */
#ifndef JANSSON_CONFIG_H
#define JANSSON_CONFIG_H

#ifdef __cplusplus
#define JSON_INLINE inline
#else
#define JSON_INLINE inline
#endif

#define JSON_INTEGER_IS_LONG_LONG 1
#define JSON_PARSER_MAX_DEPTH 2048

#endif
