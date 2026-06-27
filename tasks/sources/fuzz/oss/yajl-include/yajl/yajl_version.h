#ifndef YAJL_VERSION_H
#define YAJL_VERSION_H
#define YAJL_MAJOR 2
#define YAJL_MINOR 1
#define YAJL_MICRO 0
#define YAJL_VERSION ((YAJL_MAJOR * 10000) + (YAJL_MINOR * 100) + YAJL_MICRO)
#define YAJL_VERSION_STRING "2.1.0"
#ifdef __cplusplus
extern "C" {
#endif
extern int yajl_version(void);
#ifdef __cplusplus
}
#endif
#endif
