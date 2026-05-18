package gpuhost

import (
	"bytes"
	"os/exec"
	"strconv"
	"strings"
)

// NVIDIAGPUTemps returns GPU index → temperature °C from nvidia-smi. Empty map if unavailable.
func NVIDIAGPUTemps() map[int]float64 {
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=index,temperature.gpu",
		"--format=csv,noheader,nounits")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil
	}
	outM := make(map[int]float64)
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		idx, err0 := strconv.Atoi(strings.TrimSpace(parts[0]))
		t, err1 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		if err0 != nil || err1 != nil {
			continue
		}
		outM[idx] = t
	}
	return outM
}
