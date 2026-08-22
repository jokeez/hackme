package poolfuzz

// FluxtapFilterByteSeeds are CVE-class malformed display-filter expressions.
func FluxtapFilterByteSeeds() []any {
	return []any{
		"c73d", // \xc7=
		"3d",   // "="
		"213d", // "!="
	}
}
