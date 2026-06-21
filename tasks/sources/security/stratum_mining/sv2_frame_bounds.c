/* Property guard: stratum-mining Sv2 frame decode (same wire as BIP-370 / WarpPool).
 * Returns 1 when u24 payload exceeds 1 MiB or frame shorter than header+payload. */
typedef unsigned char u8;
typedef unsigned int u32;
typedef unsigned long long u64;

#define HDR 6u
#define MAX_PAYLOAD (1024u * 1024u)

static u8 buf[768];
static u32 buf_len;
static u32 pos;

static int too_short(u32 need) {
  return pos + need > buf_len ? 1 : 0;
}

static u32 peek_u24(void) {
  if (buf_len < HDR) return 0;
  return (u32)buf[3] | ((u32)buf[4] << 8) | ((u32)buf[5] << 16);
}

static int decode_frame_oOR(void) {
  u32 plen;
  u32 total;
  if (buf_len < HDR) return 1;
  plen = peek_u24();
  if (plen > MAX_PAYLOAD) return 1;
  total = HDR + plen;
  return buf_len < total ? 1 : 0;
}

static int compact_int_oOR(void) {
  u8 p;
  if (too_short(1)) return 1;
  p = buf[pos++];
  if (p <= 0xFC) return 0;
  if (p == 0xFD) return too_short(2);
  if (p == 0xFE) return too_short(4);
  if (p == 0xFF) return too_short(8);
  return 1;
}

static void synth(u64 n) {
  u32 i;
  buf_len = (u32)((n >> 32) & 0x2ffu);
  if (buf_len == 0) buf_len = (u32)(n % 96u) + 1u;
  if (buf_len > 768u) buf_len = 768u;
  for (i = 0; i < buf_len; i++)
    buf[i] = (u8)((n * 2654435761u + i * 17u) & 0xffu);
  pos = (u32)(n % (u64)(buf_len ? buf_len : 1u));
}

__attribute__((export_name("check")))
int check(long long n) {
  u32 op = (u32)(n & 0xffu);
  synth((u64)n);
  switch (op % 5u) {
    case 0:
      return decode_frame_oOR();
    case 1:
      return compact_int_oOR();
    case 2: {
      u32 plen = peek_u24();
      return plen > MAX_PAYLOAD ? 1 : 0;
    }
    case 3:
      return too_short(HDR);
    default:
      return decode_frame_oOR() | compact_int_oOR();
  }
}
