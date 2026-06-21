/* Property guard: stratum-mining open_channel max-target-out-of-range class.
 * Returns 1 when requested max target exceeds allowed range (simplified probe). */
typedef unsigned long long u64;

static int target_out_of_range(u64 request, u64 network_max) {
  return request > network_max ? 1 : 0;
}

__attribute__((export_name("check")))
int check(long long n) {
  u64 request = (u64)(n & 0xffffffffu);
  u64 net_max = (u64)((n >> 32) & 0xffffffffu);
  if (net_max == 0u) net_max = 0x00000000ffff0000ull;
  return target_out_of_range(request, net_max);
}
