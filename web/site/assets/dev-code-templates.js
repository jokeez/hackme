/* Shared check() source templates for developer dashboard (local CLI build, not server from_code). */
window.HackmeDevTemplates = {
  templates: {
    rust: '#[no_mangle]\npub extern "C" fn check(n: i64) -> i32 {\n    if n % 23 == 0 { 1 } else { 0 }\n}',
    c: '__attribute__((visibility("default"))) int check(long long n) {\n  return (n % 23) == 0 ? 1 : 0;\n}',
    cpp: 'extern "C" __attribute__((visibility("default"))) int check(long long n) {\n  return (n % 23) == 0 ? 1 : 0;\n}',
    tinygo:
      'package main\n\n//export check\nfunc check(n int64) int32 {\n\tif n%23 == 0 {\n\t\treturn 1\n\t}\n\treturn 0\n}\n\nfunc main() {}\n',
    zig: 'export fn check(n: i64) i32 {\n    if (@rem(n, 23) == 0) return 1;\n    return 0;\n}\n',
    assemblyscript: 'export function check(n: i64): i32 {\n  return i32((n % 23) == 0 ? 1 : 0);\n}\n',
    wat: '(module\n  (func (export "check") (param i64) (result i32)\n    local.get 0\n    i64.const 23\n    i64.rem_s\n    i64.eqz\n    if (result i32) i32.const 1 else i32.const 0 end))',
  },
  extForLang(lang) {
    const m = {
      rust: "rs",
      c: "c",
      cpp: "cpp",
      tinygo: "go",
      zig: "zig",
      assemblyscript: "ts",
      wat: "wat",
    };
    return m[String(lang || "rust").toLowerCase()] || "rs";
  },
  apply(lang, textarea) {
    if (!textarea) return;
    const L = String(lang || "rust").toLowerCase();
    textarea.value = this.templates[L] || this.templates.rust;
  },
  buildCLI(lang, opts) {
    const ext = this.extForLang(lang);
    const id = opts.id || "my-order-001";
    const out = opts.outDir || "./fuzzing-out";
    return (
      "hackme-fuzzing build -lang " +
      String(lang || "rust") +
      " -source check." +
      ext +
      " -out " +
      out +
      " -id " +
      id +
      " -reward " +
      (opts.reward != null ? opts.reward : "0.01") +
      " -difficulty " +
      (opts.difficulty != null ? opts.difficulty : "10") +
      " -target " +
      (opts.target != null ? opts.target : "3")
    );
  },
};
