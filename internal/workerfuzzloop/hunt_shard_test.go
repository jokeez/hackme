package workerfuzzloop

import "testing"

func TestIsHuntClaim(t *testing.T) {
	if !IsHuntClaim(ClaimResp{TaskClass: "hunt"}) {
		t.Fatal("task_class hunt")
	}
	if !IsHuntClaim(ClaimResp{WorkKind: "hunt_shard"}) {
		t.Fatal("work_kind hunt_shard")
	}
	if IsHuntClaim(ClaimResp{TaskClass: "fuzz"}) {
		t.Fatal("fuzz is not hunt")
	}
}

func TestHuntClaimMissingFields(t *testing.T) {
	if HuntClaimMissingFields(ClaimResp{UpstreamTargetID: "x", InputBytesHex: "ab"}) != nil {
		t.Fatal("complete claim")
	}
	if HuntClaimMissingFields(ClaimResp{InputBytesHex: "ab"}) == nil {
		t.Fatal("missing target")
	}
}
