/* Property guard: mkpool::stratum::validate_version (stratum_protocol.cpp).
 * Returns 1 when submitted version violates negotiated mask. */
typedef unsigned int u32;

static int validate_version(u32 submitted, u32 templ, u32 mask) {
  return ((submitted & ~mask) == (templ & ~mask)) ? 0 : 1;
}

__attribute__((export_name("check")))
int check(long long n) {
  u32 submitted = (u32)(n & 0xffffffffu);
  u32 templ = (u32)((n >> 32) & 0xffffffffu);
  u32 mask = (u32)((n >> 8) & 0xffffffffu);
  if (mask == 0u) mask = 0x1fffe000u; /* typical pool mask */
  return validate_version(submitted, templ, mask);
}
