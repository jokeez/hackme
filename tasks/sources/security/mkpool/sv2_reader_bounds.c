/* Property guard: mkpool sv2::Reader bounds (sv2_codec.hpp).
 * Returns 1 when a read would throw out_of_range or B0_32 too long. */
typedef unsigned char u8;
typedef unsigned short u16;
typedef unsigned int u32;
typedef unsigned long long u64;

static u8 buf[512];
static u32 buf_len;
static u32 pos;

static int would_oOR(u32 need) {
  return pos + need > buf_len;
}

static u8 read_u8(void) {
  if (would_oOR(1)) return 0;
  return buf[pos++];
}

static u16 read_u16(void) {
  if (would_oOR(2)) return 0;
  u16 v = (u16)(buf[pos] | (buf[pos + 1] << 8));
  pos += 2;
  return v;
}

static u32 read_u32(void) {
  if (would_oOR(4)) return 0;
  u32 v = (u32)(buf[pos] | (buf[pos + 1] << 8) | (buf[pos + 2] << 16) | (buf[pos + 3] << 24));
  pos += 4;
  return v;
}

static int read_b0_32_oOR(void) {
  u8 len = read_u8();
  if (len > 32) return 2; /* runtime_error B0_32 too long */
  if (would_oOR(len)) return 1;
  pos += len;
  return 0;
}

static int read_str0_255_oOR(void) {
  u8 len = read_u8();
  if (would_oOR(len)) return 1;
  pos += len;
  return 0;
}

static int read_b0_64k_oOR(void) {
  u16 len = read_u16();
  if (would_oOR(len)) return 1;
  pos += len;
  return 0;
}

static void synth_buffer(u64 n) {
  u32 i;
  buf_len = (u32)((n >> 32) & 0x1ffu);
  if (buf_len == 0) buf_len = (u32)(n % 64u) + 1u;
  if (buf_len > 512u) buf_len = 512u;
  for (i = 0; i < buf_len; i++) buf[i] = (u8)((n * 1315423911u + i * 37u) & 0xffu);
  pos = (u32)(n % (u64)buf_len);
}

__attribute__((export_name("check")))
int check(long long n) {
  u32 op = (u32)(n & 0xffu);
  int r;
  synth_buffer((u64)n);
  if (buf_len == 0) return 0;
  switch (op % 7u) {
    case 0:
      return would_oOR(1) ? 1 : 0;
    case 1:
      return would_oOR(2) ? 1 : 0;
    case 2:
      return would_oOR(4) ? 1 : 0;
    case 3:
      return would_oOR(8) ? 1 : 0;
    case 4:
      r = read_b0_32_oOR();
      return r != 0 ? 1 : 0;
    case 5:
      return read_str0_255_oOR() ? 1 : 0;
    default:
      return read_b0_64k_oOR() ? 1 : 0;
  }
}
