# libucl disclosure draft

**Status:** REPORTED — https://github.com/vstakhov/libucl/issues/396

**Where:** https://github.com/vstakhov/libucl/issues/new  
**Security contact (if preferred):** maintainer is active on GitHub (vstakhov)

**Title:** `UBSan: incorrect function pointer type in ucl_hash_foreach during ucl_parser_free (concatenated input)`

---

## Summary

When parsing input that contains **two consecutive JSON/UCL documents** in one chunk, `ucl_parser_free()` triggers **UndefinedBehaviorSanitizer** in `ucl_hash.c:275`: a callback is invoked through `void (*)(void *)` but the actual target is `ucl_object_dtor_unref(ucl_object_t *)` (incompatible function type / CWE-843).

This is **reproducible on current master** (`93beea87cf36b2f4300c99c135bcad46a5aa237c`, 2026-06-26).

**Severity (our triage):** Low — UBSan-class undefined behavior on parser teardown. Not observed as ASan heap corruption in this path. May be denial-of-service or latent memory-safety risk on builds without sanitizers.

**CVE:** Request only if maintainer agrees it is security-relevant (similar libucl issues received CVE-2025-6499 / CVE-2025-11010 for heap issues; this is a **different** bug class).

---

## Minimal repro

```bash
git clone https://github.com/vstakhov/libucl
cd libucl && git checkout 93beea87cf36b2f4300c99c135bcad46a5aa237c
# build stdin driver with -fsanitize=undefined (see tasks/sources/fuzz/oss/libucl_stdin.c)
echo -n '{"a":1}{"a":1}' | ./libucl_stdin
```

**Expected:** UBSan report:

```
ucl_hash.c:275: runtime error: call to function ucl_object_dtor_unref through pointer to incorrect function type 'void (*)(void *)'
```

**Minimized input (14 bytes):** `{"a":1}{"a":1}`

Artifact: `reports/oss-cve/hold-deep-20260626T070302Z/libucl/crashes/crash-libucl-7b2261223a317d7b.bin`

---

## Suggested fix direction

In `ucl_hash_foreach` (`ucl_hash.c`), ensure the destructor callback type matches `ucl_object_dtor_unref` (e.g. use a typed function pointer or a thin wrapper with correct signature) instead of casting through `void (*)(void *)`.

---

## Issue body (copy-paste)

```
### Description

Parsing two JSON objects concatenated in a single buffer, then freeing the parser, triggers UBSan on master.

### Environment

- libucl commit: 93beea87cf36b2f4300c99c135bcad46a5aa237c
- Compiler: clang with `-fsanitize=undefined`
- OS: Linux

### Steps to reproduce

1. `ucl_parser_new` → `ucl_parser_add_chunk(parser, "{\"a\":1}{\"a\":1}", 14)` → `ucl_parser_get_object` → `ucl_object_unref` (if non-null) → `ucl_parser_free`
2. Observe UBSan at `src/ucl_hash.c:275` during hash teardown.

### Actual behavior

UBSan: call to `ucl_object_dtor_unref` through incompatible function pointer type `void (*)(void *)`.

### Expected behavior

Clean parser teardown without undefined behavior.

### Impact

Undefined behavior on free path with untrusted concatenated documents. We have not demonstrated heap corruption on ASan for this specific input; treating as low-severity hardening unless you see otherwise.

### Credit

Found via structured mutation fuzz (HackMe OSS CVE hunt). Happy to coordinate CVE assignment if you consider it security-relevant.
```

**Do not** post PoC on social media until maintainer triage or 90-day window.
