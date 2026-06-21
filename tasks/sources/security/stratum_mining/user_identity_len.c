/* Property guard: stratum-mining TLV user identity MAX_USER_IDENTITY_LENGTH = 32.
 * Returns 1 when identity bytes exceed extension limit. */
typedef unsigned long long u64;

#define MAX_USER_IDENTITY 32u

static int identity_too_long(u64 len) {
  return len > (u64)MAX_USER_IDENTITY ? 1 : 0;
}

__attribute__((export_name("check")))
int check(long long n) {
  u64 len = (u64)(n & 0x7fu);
  u64 extra = (u64)((n >> 10) & 0x3fu);
  return identity_too_long(len + extra);
}
