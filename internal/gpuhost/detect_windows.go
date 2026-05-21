//go:build windows

package gpuhost

import (
	"os/exec"
	"strings"
)

func detectHostGPUs() HostGPUReport {
	var rep HostGPUReport
	names := windowsVideoControllerNames()
	for _, n := range names {
		mergeReportNames(&rep, n)
	}
	return finalizeHostReport(rep)
}

func windowsVideoControllerNames() []string {
	out, err := exec.Command("wmic", "path", "win32_VideoController", "get", "name").Output()
	if err != nil {
		return nil
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "Name") {
			continue
		}
		if strings.Contains(strings.ToLower(line), "microsoft basic") {
			continue
		}
		names = append(names, line)
	}
	return names
}
