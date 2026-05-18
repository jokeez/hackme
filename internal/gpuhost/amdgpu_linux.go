//go:build linux

package gpuhost

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var drmCardNameRe = regexp.MustCompile(`^card([0-9]+)$`)

// AMDGPUCardTelemetry is one amdgpu DRM card from sysfs (temp, busy, power when exposed).
type AMDGPUCardTelemetry struct {
	Index        int
	Card         string
	Name         string
	TempC        float64
	BusyPct      float64
	PowerDrawW   float64
	PowerAverage bool // true when power1_average was read
}

// ListAMDGPUTelemetry enumerates cards as card0, card1, … (by number) that use the amdgpu driver.
// Index is 0..n-1 in that order (same convention as OpenCL GPU #0 on typical single-GPU desktops).
func ListAMDGPUTelemetry() []AMDGPUCardTelemetry {
	ents, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return nil
	}
	type cardID struct {
		num int
		dir string
	}
	var ids []cardID
	for _, e := range ents {
		if e.IsDir() && drmCardNameRe.MatchString(e.Name()) {
			m := drmCardNameRe.FindStringSubmatch(e.Name())
			if len(m) < 2 {
				continue
			}
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			root := filepath.Join("/sys/class/drm", e.Name(), "device")
			if !isAmdgpuDevice(root) {
				continue
			}
			ids = append(ids, cardID{num: n, dir: root})
		}
	}
	if len(ids) == 0 {
		return nil
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].num < ids[j].num })
	out := make([]AMDGPUCardTelemetry, 0, len(ids))
	for idx, c := range ids {
		t := readAMDGPUCardTelemetry(idx, "card"+strconv.Itoa(c.num), c.dir)
		out = append(out, t)
	}
	return out
}

func isAmdgpuDevice(deviceRoot string) bool {
	link, err := filepath.EvalSymlinks(filepath.Join(deviceRoot, "driver"))
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Base(link), "amdgpu")
}

func readAMDGPUCardTelemetry(index int, card, deviceRoot string) AMDGPUCardTelemetry {
	t := AMDGPUCardTelemetry{Index: index, Card: card, BusyPct: -1}
	if b, err := os.ReadFile(filepath.Join(deviceRoot, "product_name")); err == nil {
		t.Name = strings.TrimSpace(string(b))
	}
	if t.Name == "" {
		t.Name = "AMD GPU (" + card + ")"
	}
	t.TempC = maxHwmonTempC(deviceRoot)
	if b, err := os.ReadFile(filepath.Join(deviceRoot, "gpu_busy_percent")); err == nil {
		if v, err := strconv.ParseFloat(strings.TrimSpace(string(b)), 64); err == nil && v >= 0 {
			t.BusyPct = v
		}
	}
	if pW, ok := readAmdgpuPowerDrawW(deviceRoot); ok {
		t.PowerDrawW = pW
		t.PowerAverage = true
	}
	return t
}

func maxHwmonTempC(deviceRoot string) float64 {
	pattern := filepath.Join(deviceRoot, "hwmon", "hwmon*", "temp*_input")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return -1
	}
	var best float64 = -1
	for _, p := range matches {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		mv, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if err != nil || mv <= 0 {
			continue
		}
		tc := float64(mv) / 1000.0
		if tc > best {
			best = tc
		}
	}
	return best
}

func readAmdgpuPowerDrawW(deviceRoot string) (float64, bool) {
	pattern := filepath.Join(deviceRoot, "hwmon", "hwmon*", "power1_average")
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return 0, false
	}
	for _, p := range matches {
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		uw, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
		if err != nil || uw <= 0 {
			continue
		}
		return float64(uw) / 1e6, true
	}
	return 0, false
}
