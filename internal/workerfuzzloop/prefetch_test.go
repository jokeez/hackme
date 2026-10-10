package workerfuzzloop

import "testing"

func TestPrefetchBufTakePut(t *testing.T) {
	var buf prefetchBuf
	cr := &ClaimResp{OK: true, CampaignID: "c", ItemID: 1}
	if !buf.tryPut(cr) {
		t.Fatal("first put")
	}
	if buf.tryPut(&ClaimResp{ItemID: 2}) {
		t.Fatal("second put must fail while occupied")
	}
	got := buf.take()
	if got == nil || got.ItemID != 1 {
		t.Fatalf("take=%v", got)
	}
	if buf.take() != nil {
		t.Fatal("empty take")
	}
}
