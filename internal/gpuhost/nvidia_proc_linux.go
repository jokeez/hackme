//go:build linux

package gpuhost

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// NVIDIAProcCard is a GPU enumerated via /proc when nvidia-smi/NVML is unavailable
// (e.g. driver/library version mismatch after a driver update without reboot).
type NVIDIAProcCard struct {
	Index int
	PCI   string
	Name  string
}

var nvidiaProcModelRe = regexp.MustCompile(`(?m)^Model:\s+(.+)$`)

// ListNVIDIAProcCards reads /proc/driver/nvidia/gpus/*/information for model names.
func ListNVIDIAProcCards() []NVIDIAProcCard {
	ents, err := os.ReadDir("/proc/driver/nvidia/gpus")
	if err != nil {
		return nil
	}
	type pciEntry struct {
		pci  string
		name string
	}
	var list []pciEntry
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		pci := strings.TrimSpace(e.Name())
		if pci == "" {
			continue
		}
		infoPath := filepath.Join("/proc/driver/nvidia/gpus", pci, "information")
		b, err := os.ReadFile(infoPath)
		if err != nil {
			continue
		}
		m := nvidiaProcModelRe.FindStringSubmatch(string(b))
		if len(m) < 2 {
			continue
		}
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		list = append(list, pciEntry{pci: pci, name: name})
	}
	if len(list) == 0 {
		return nil
	}
	sort.Slice(list, func(i, j int) bool { return list[i].pci < list[j].pci })
	out := make([]NVIDIAProcCard, 0, len(list))
	for i, e := range list {
		out = append(out, NVIDIAProcCard{Index: i, PCI: e.pci, Name: e.name})
	}
	return out
}
