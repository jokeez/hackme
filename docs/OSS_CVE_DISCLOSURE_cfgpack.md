# cfgpack disclosure draft

**Status:** REPORTED — https://github.com/Arsievert/cfgpack/issues/2

**Where:** https://github.com/Arsievert/cfgpack/issues/new

**Title:** `UBSan: signed integer overflow in cfgpack_msgpack_decode_int64 (msgpack 0xd3 path)`

---

## Summary

`cfgpack_msgpack_decode_int64()` accumulates an 8-byte big-endian integer with:

```c
res = (res << 8) | v[i];  // msgpack.c:294
```

On certain malformed/truncated inputs, **signed left-shift overflows `int64_t`** before the loop completes → UBSan (CWE-190).

**Commit tested:** `957385acee237d9adb3a7abb1e346f197f83aa9d` (master, 2026-06-26)

**Severity (our triage):** Low — undefined behavior in decoder on hostile msgpack bytes. No ASan heap corruption observed. Typical fix: accumulate in `uint64_t` then bounds-check before casting to `int64_t`, or validate prefix bytes before shift.

**CVE:** Unlikely standalone CVE unless maintainer escalates; still worth reporting as security hardening (similar patterns fixed in msgpack-c).

---

## Minimal repro

```bash
git clone https://github.com/Arsievert/cfgpack
cd cfgpack && git checkout 957385acee237d9adb3a7abb1e346f197f83aa9d
# build tasks/sources/fuzz/oss/cfgpack_stdin.c with -fsanitize=undefined
xxd -r -p <<< d4d6f79fd691b31d73d3c7a963d32a03d4d6f79fd691b3 | ./cfgpack_stdin
```

**Expected:**

```
msgpack.c:294: runtime error: left shift of ... by 8 places cannot be represented in type 'int64_t'
```

Artifact: `reports/oss-cve/hold-deep-20260626T070302Z/cfgpack/crashes/crash-cfgpack-d4d6f79fd691b31d.bin` (23 bytes)

---

## Issue body (copy-paste)

```
### Description

Fuzzing `cfgpack_msgpack_decode_int64` with UBSan reports signed integer overflow when decoding certain hostile msgpack byte sequences (0xd3 int64 wire format).

### Environment

- cfgpack commit: 957385acee237d9adb3a7abb1e346f197f83aa9d
- clang `-fsanitize=undefined`
- Linux

### Steps to reproduce

Feed 23-byte input (hex): `d4d6f79fd691b31d73d3c7a963d32a03d4d6f79fd691b3`
to a harness that calls `cfgpack_msgpack_decode_int64` in a loop (see cfgpack_stdin pattern).

### Actual behavior

UBSan at `src/msgpack.c:294` — left shift overflow on `int64_t res`.

### Expected behavior

Decoder returns `CFGPACK_ERR_DECODE` without undefined behavior.

### Suggested fix

Accumulate in `uint64_t`, check high-bit / range before assigning to `int64_t`; or reject invalid prefix before shifting.

### Impact

Low — UB on malicious msgpack input to decode helpers. No heap corruption demonstrated.

### Credit

HackMe OSS CVE hunt (mutation fuzz, 150k iterations). Happy to help validate a patch.
```

**Do not** publish exploit bytes on social until maintainer triage.
