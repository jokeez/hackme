package gputune

import "testing"

func TestForGPUNamePolaris2048SPFull(t *testing.T) {
	name := "Advanced Micro Devices, Inc. Polaris 20 XL Radeon RX 580 2048SP"
	h := ForGPUName(name)
	if h.Family != "AMD Polaris/Vega" {
		t.Fatalf("family=%q want AMD Polaris/Vega", h.Family)
	}
}
