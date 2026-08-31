package hunt

import "testing"

func TestSuggestPacksXML(t *testing.T) {
	sugs := SuggestPacksForPath("src/xml_parse.c", "<?xml version=\"1.0\"?>")
	found := false
	for _, s := range sugs {
		if s.PackID == "parser_expat" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected parser_expat in %v", sugs)
	}
}

func TestSuggestPacksHuntReuse(t *testing.T) {
	sugs := SuggestPacksForPath("fuzz/target.c", "int LLVMFuzzerTestOneInput(const uint8_t*d,size_t n){return 0;}")
	found := false
	for _, s := range sugs {
		if s.PackID == "hunt_reuse" && s.Product == "hunt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected hunt_reuse in %v", sugs)
	}
}
