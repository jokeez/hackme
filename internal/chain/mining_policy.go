package chain

import (
	"sort"

	"hackme/internal/gpupoh"
)

// MiningDevicePolicy controls which accelerators participate in local PoH mining.
// The dashboard persists profile_mode and per-device enable/priority in SQLite; main wires
// these callbacks before miner.Start and after POST /api/mining/devices.
type MiningDevicePolicy struct {
	Profile     string // mixed | cpu | gpu
	GPUEnabled  func(backend string, index int) bool
	GPUPriority func(backend string, index int) int
	CPUEnabled  func() bool
}

func defaultMiningDevicePolicy() MiningDevicePolicy {
	return MiningDevicePolicy{
		Profile: "mixed",
		GPUEnabled: func(string, int) bool {
			return true
		},
		GPUPriority: func(string, int) int {
			return 100
		},
		CPUEnabled: func() bool {
			return true
		},
	}
}

func (m *Miner) SetMiningDevicePolicy(p MiningDevicePolicy) {
	m.policyMu.Lock()
	defer m.policyMu.Unlock()
	if p.Profile == "" {
		p.Profile = "mixed"
	}
	if p.GPUEnabled == nil {
		p.GPUEnabled = defaultMiningDevicePolicy().GPUEnabled
	}
	if p.GPUPriority == nil {
		p.GPUPriority = defaultMiningDevicePolicy().GPUPriority
	}
	if p.CPUEnabled == nil {
		p.CPUEnabled = defaultMiningDevicePolicy().CPUEnabled
	}
	m.devicePolicy = p
}

func (m *Miner) miningPolicy() MiningDevicePolicy {
	m.policyMu.RLock()
	defer m.policyMu.RUnlock()
	if m.devicePolicy.Profile == "" && m.devicePolicy.GPUEnabled == nil {
		return defaultMiningDevicePolicy()
	}
	p := m.devicePolicy
	if p.Profile == "" {
		p.Profile = "mixed"
	}
	if p.GPUEnabled == nil {
		p.GPUEnabled = defaultMiningDevicePolicy().GPUEnabled
	}
	if p.GPUPriority == nil {
		p.GPUPriority = defaultMiningDevicePolicy().GPUPriority
	}
	if p.CPUEnabled == nil {
		p.CPUEnabled = defaultMiningDevicePolicy().CPUEnabled
	}
	return p
}

type gpuAccelSort struct {
	idx      int
	priority int
}

func sortAcceleratorsByPriority(accs []gpupoh.Accelerator, priority func(backend string, index int) int) []int {
	if len(accs) <= 1 {
		out := make([]int, len(accs))
		for i := range accs {
			out[i] = i
		}
		return out
	}
	order := make([]gpuAccelSort, len(accs))
	for i, a := range accs {
		p := 100
		if priority != nil {
			p = priority(a.Backend(), a.DeviceIndex())
		}
		order[i] = gpuAccelSort{idx: i, priority: p}
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i].priority == order[j].priority {
			return order[i].idx < order[j].idx
		}
		return order[i].priority > order[j].priority
	})
	out := make([]int, len(order))
	for i, o := range order {
		out[i] = o.idx
	}
	return out
}
