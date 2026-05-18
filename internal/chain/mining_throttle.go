package chain

import (
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
)

// maybeSleepIfHostCPUHigh samples host CPU and briefly sleeps when it stays above
// throttlePct (soft cap). Called rarely from each worker to avoid gopsutil overhead.
func (m *Miner) maybeSleepIfHostCPUHigh(batchIndex uint64) {
	t := m.softThrottlePct()
	if t <= 0 || t >= 99.5 {
		return
	}
	if batchIndex&0x1f != 0 { // every 32 outer batches (~3.2M nonces per worker)
		return
	}
	p, err := cpu.Percent(0, false)
	if err != nil || len(p) == 0 || p[0] <= t+1.5 { // small hysteresis vs target
		return
	}
	_, _ = cpu.Percent(25*time.Millisecond, false)
	time.Sleep(10 * time.Millisecond)
}
