/* Property guard: ckpool parse_submit ntime rolling window (+7000 sec).
 * Returns 1 when ntime32 < wb->ntime32 or ntime32 > wb->ntime32 + 7000. */
typedef unsigned int u32;

static int ntime_out_of_window(u32 ntime32, u32 wb_ntime32) {
  if (ntime32 < wb_ntime32) return 1;
  if (ntime32 > wb_ntime32 + 7000u) return 1;
  return 0;
}

__attribute__((export_name("check")))
int check(long long n) {
  u32 wb_ntime = (u32)((n >> 32) & 0xffffffffu);
  u32 ntime32 = (u32)(n & 0xffffffffu);
  if (wb_ntime == 0u) wb_ntime = 0x5f000000u;
  return ntime_out_of_window(ntime32, wb_ntime);
}
