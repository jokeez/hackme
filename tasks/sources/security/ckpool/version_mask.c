/* Property guard: ckpool stratifier.c parse_submit version-rolling mask.
 * Returns 1 when (~pool_mask & submitted) != 0 (SE_INVALID_VERSION_MASK). */
typedef unsigned int u32;

static int invalid_version_mask(u32 submitted, u32 pool_mask) {
  if (submitted == 0u) return 0;
  return ((~pool_mask) & submitted) != 0u ? 1 : 0;
}

__attribute__((export_name("check")))
int check(long long n) {
  u32 submitted = (u32)(n & 0xffffffffu);
  u32 pool_mask = (u32)((n >> 32) & 0xffffffffu);
  if (pool_mask == 0u) pool_mask = 0x1fffe000u;
  return invalid_version_mask(submitted, pool_mask);
}
