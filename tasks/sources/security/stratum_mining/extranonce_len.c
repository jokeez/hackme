/* Property guard: stratum-mining channels-sv2 MAX_EXTRANONCE_LEN = 32.
 * Returns 1 when extranonce prefix exceeds spec limit. */
typedef unsigned long long u64;

#define MAX_EXTRANONCE_LEN 32u

static int prefix_too_long(u64 len) {
  return len > (u64)MAX_EXTRANONCE_LEN ? 1 : 0;
}

__attribute__((export_name("check")))
int check(long long n) {
  u64 claimed = (u64)(n & 0x3fu);
  u64 inflate = (u64)((n >> 8) & 0xffu);
  u64 total = claimed + inflate;
  return prefix_too_long(total);
}
