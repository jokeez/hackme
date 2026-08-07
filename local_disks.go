package main

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/shirou/gopsutil/v3/disk"
)

const (
	hmsMinQuotaGB      = 50
	hmsReservePct      = 0.15
	hmsReserveSystemGB = 20
	hmsReserveDataGB   = 10
)

// LocalDiskInfo describes a host drive suitable for HMS storage planning.
type LocalDiskInfo struct {
	ID                 string  `json:"id"`
	Device             string  `json:"device"`
	Model              string  `json:"model"`
	Mount              string  `json:"mount"`
	Filesystem         string  `json:"filesystem"`
	SizeGB             float64 `json:"size_gb"`
	UsedGB             float64 `json:"used_gb"`
	FreeGB             float64 `json:"free_gb"`
	UsedPct            float64 `json:"used_pct"`
	AllocatableGB      float64 `json:"allocatable_gb"`
	ReserveGB          float64 `json:"reserve_gb"`
	SystemDisk         bool    `json:"system_disk"`
	Removable          bool    `json:"removable"`
	Mounted            bool    `json:"mounted"`
	StoragePathSuggest string  `json:"storage_path_suggest"`
	HMSAllocatedGB     float64 `json:"hms_allocated_gb"`
	HMSActive          bool    `json:"hms_active"`
}

type localDisksResponse struct {
	Status     string          `json:"status"`
	ReserveGB  float64         `json:"reserve_gb"`
	MinQuotaGB int             `json:"min_quota_gb"`
	Disks      []LocalDiskInfo `json:"disks"`
}

type lsblkNode struct {
	Name       string      `json:"name"`
	Type       string      `json:"type"`
	Size       uint64      `json:"size"`
	Fstype     string      `json:"fstype"`
	Mountpoint string      `json:"mountpoint"`
	Model      string      `json:"model"`
	Rm         bool        `json:"rm"`
	Children   []lsblkNode `json:"children"`
}

type lsblkJSON struct {
	Blockdevices []lsblkNode `json:"blockdevices"`
}

func (a *app) handleLocalDisks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Host inventory is operator-local: loopback or admin token only (not public hub).
	if !requestFromLoopback(r) && !(adminAuthEnabled() && adminRequestAuthed(r)) {
		http.Error(w, "local disks require loopback or admin auth", http.StatusForbidden)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	root := resolveWorkerRepoRoot(strings.TrimSpace(a.dataDir))
	disks, err := collectLocalDisks(root)
	if err != nil {
		writeJSON(w, map[string]any{"status": "error", "error": err.Error()})
		return
	}
	writeJSON(w, localDisksResponse{
		Status:     "ok",
		ReserveGB:  hmsReserveSystemGB,
		MinQuotaGB: hmsMinQuotaGB,
		Disks:      disks,
	})
}

func collectLocalDisks(root string) ([]LocalDiskInfo, error) {
	if root == "" {
		root = repoRoot()
	}
	hmsUsage := scanHMSStorageUsage(root)
	if runtime.GOOS == "linux" {
		if disks, err := collectDisksLinux(root, hmsUsage); err == nil && len(disks) > 0 {
			sortDisks(disks)
			return disks, nil
		}
	}
	disks, err := collectDisksPartitions(root, hmsUsage)
	sortDisks(disks)
	return disks, err
}

func collectDisksLinux(root string, hmsUsage map[string]float64) ([]LocalDiskInfo, error) {
	out, err := exec.Command("lsblk", "-J", "-b", "-o", "NAME,TYPE,SIZE,FSTYPE,MOUNTPOINT,MODEL,RM").Output()
	if err != nil {
		return nil, err
	}
	var doc lsblkJSON
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, err
	}
	var disks []LocalDiskInfo
	for _, dev := range doc.Blockdevices {
		if dev.Type != "disk" {
			continue
		}
		mount, fstype, partDev := bestMountOnDisk(dev)
		info := LocalDiskInfo{
			ID:        dev.Name,
			Device:    "/dev/" + dev.Name,
			Model:     strings.TrimSpace(dev.Model),
			Removable: dev.Rm,
			Mounted:   mount != "",
		}
		if info.Model == "" {
			info.Model = dev.Name
		}
		info.SizeGB = round2(float64(dev.Size) / (1024 * 1024 * 1024))
		if mount != "" {
			info.Mount = mount
			info.Device = partDev
			info.Filesystem = fstype
			info.SystemDisk = mount == "/" || strings.HasPrefix(mount, "/boot")
			fillUsage(&info)
			info.StoragePathSuggest = suggestHMSStoragePath(mount, dev.Name, root)
		}
		applyHMSUsage(&info, hmsUsage)
		disks = append(disks, info)
	}
	return disks, nil
}

func bestMountOnDisk(dev lsblkNode) (mount, fstype, partDev string) {
	var candidates []struct {
		mount, fstype, partDev string
		free                   float64
	}
	var walk func(lsblkNode)
	walk = func(n lsblkNode) {
		mp := strings.TrimSpace(n.Mountpoint)
		if mp != "" && isStorageFstype(n.Fstype) {
			free := 0.0
			if u, err := disk.Usage(mp); err == nil && u != nil {
				free = float64(u.Free)
			}
			candidates = append(candidates, struct {
				mount, fstype, partDev string
				free                   float64
			}{mp, n.Fstype, "/dev/" + n.Name, free})
		}
		for _, ch := range n.Children {
			walk(ch)
		}
	}
	walk(dev)
	if len(candidates) == 0 {
		return "", "", ""
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].free > candidates[j].free })
	best := candidates[0]
	return best.mount, best.fstype, best.partDev
}

func collectDisksPartitions(root string, hmsUsage map[string]float64) ([]LocalDiskInfo, error) {
	parts, err := disk.Partitions(true)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var disks []LocalDiskInfo
	for _, p := range parts {
		if !isStorageFstype(p.Fstype) {
			continue
		}
		mount := strings.TrimSpace(p.Mountpoint)
		if mount == "" || seen[mount] {
			continue
		}
		seen[mount] = true
		id := strings.TrimPrefix(filepath.Base(p.Device), "")
		if id == "" {
			id = mount
		}
		info := LocalDiskInfo{
			ID:         id,
			Device:     p.Device,
			Mount:      mount,
			Filesystem: p.Fstype,
			Mounted:    true,
			SystemDisk: mount == "/" || strings.HasPrefix(mount, "/boot"),
		}
		fillUsage(&info)
		info.StoragePathSuggest = suggestHMSStoragePath(mount, id, root)
		applyHMSUsage(&info, hmsUsage)
		disks = append(disks, info)
	}
	return disks, nil
}

func fillUsage(info *LocalDiskInfo) {
	u, err := disk.Usage(info.Mount)
	if err != nil || u == nil {
		return
	}
	info.SizeGB = round2(float64(u.Total) / (1024 * 1024 * 1024))
	info.UsedGB = round2(float64(u.Used) / (1024 * 1024 * 1024))
	info.FreeGB = round2(float64(u.Free) / (1024 * 1024 * 1024))
	if u.Total > 0 {
		info.UsedPct = round2(u.UsedPercent)
	}
	reserve := float64(hmsReserveDataGB)
	if info.SystemDisk {
		reserve = float64(hmsReserveSystemGB)
	}
	if r := info.FreeGB * hmsReservePct; r > reserve {
		reserve = round2(r)
	}
	info.ReserveGB = reserve
	info.AllocatableGB = round2(max(0, info.FreeGB-reserve))
}

func applyHMSUsage(info *LocalDiskInfo, usage map[string]float64) {
	if gb, ok := usage[info.Mount]; ok && gb > 0 {
		info.HMSAllocatedGB = round2(gb)
		info.HMSActive = true
	}
}

func isStorageFstype(fs string) bool {
	fs = strings.ToLower(strings.TrimSpace(fs))
	switch fs {
	case "", "tmpfs", "devtmpfs", "squashfs", "overlay", "autofs", "cgroup2", "proc", "sysfs":
		return false
	}
	return true
}

func suggestHMSStoragePath(mount, diskID, root string) string {
	if mount == "" || mount == "/" {
		return filepath.Join(root, "data", "hms_storage_"+diskID)
	}
	return filepath.Join(mount, "hackme-hms-storage")
}

func repoRoot() string {
	if v := strings.TrimSpace(os.Getenv("HACKME_ROOT")); v != "" {
		return v
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}

func scanHMSStorageUsage(root string) map[string]float64 {
	out := map[string]float64{}
	dataDir := filepath.Join(root, "data")
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, "hms_storage") && !strings.HasPrefix(name, "hms-storage") {
			continue
		}
		dir := filepath.Join(dataDir, name)
		mount := mountForPath(dir)
		if mount == "" {
			continue
		}
		var total int64
		_ = filepath.Walk(dir, func(path string, fi os.FileInfo, err error) error {
			if err != nil || fi == nil || fi.IsDir() {
				return nil
			}
			total += fi.Size()
			return nil
		})
		out[mount] += float64(total) / (1024 * 1024 * 1024)
	}
	return out
}

func mountForPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	parts, err := disk.Partitions(true)
	if err != nil {
		return ""
	}
	best := ""
	bestLen := 0
	for _, p := range parts {
		m := strings.TrimSpace(p.Mountpoint)
		if m == "" || !strings.HasPrefix(abs, m) {
			continue
		}
		if len(m) > bestLen {
			best = m
			bestLen = len(m)
		}
	}
	return best
}

func sortDisks(disks []LocalDiskInfo) {
	sort.Slice(disks, func(i, j int) bool {
		if disks[i].SystemDisk != disks[j].SystemDisk {
			return !disks[i].SystemDisk
		}
		return disks[i].AllocatableGB > disks[j].AllocatableGB
	})
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
