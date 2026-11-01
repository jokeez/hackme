// Nested module so `go test ./...` from the repo root never walks
// desktop toolchain trees under logs/ (tinygo/src, etc.).
module hackme.logs.ignore

go 1.22
