package main

import (
	"encoding/json"
	"testing"
)

func TestBestMountOnDiskPicksMostFree(t *testing.T) {
	orig := diskUsageFree
	t.Cleanup(func() { diskUsageFree = orig })
	diskUsageFree = func(mp string) (uint64, bool) {
		switch mp {
		case "/boot/efi":
			return 400_000_000, true
		case "/":
			return 200_000_000_000, true
		}
		return 0, false
	}
	var doc lsblkJSON
	if err := json.Unmarshal([]byte(`{
		"blockdevices":[{"name":"sda","type":"disk","size":500000000000,"children":[
			{"name":"sda1","type":"part","fstype":"vfat","mountpoint":"/boot/efi","size":500000000},
			{"name":"sda2","type":"part","fstype":"ext4","mountpoint":"/","size":499500000000}
		]}]
	}`), &doc); err != nil {
		t.Fatal(err)
	}
	mount, fstype, dev := bestMountOnDisk(doc.Blockdevices[0])
	if mount != "/" || fstype != "ext4" || dev != "/dev/sda2" {
		t.Fatalf("got mount=%q fstype=%q dev=%q", mount, fstype, dev)
	}
}

func TestBestMountOnDiskUnknownUsagePrefersLargerPartition(t *testing.T) {
	orig := diskUsageFree
	t.Cleanup(func() { diskUsageFree = orig })
	diskUsageFree = func(string) (uint64, bool) { return 0, false }
	root := lsblkNode{Name: "sda", Type: "disk", Children: []lsblkNode{
		{Name: "sda1", Type: "part", Fstype: "vfat", Mountpoint: "/boot/efi", Size: 500_000_000},
		{Name: "sda2", Type: "part", Fstype: "ext4", Mountpoint: "/", Size: 499_500_000_000},
	}}
	if mount, _, dev := bestMountOnDisk(root); mount != "/" || dev != "/dev/sda2" {
		t.Fatalf("got mount=%q dev=%q", mount, dev)
	}
}

func TestDiskHasSystemMountIgnoresPickedPartition(t *testing.T) {
	orig := diskUsageFree
	t.Cleanup(func() { diskUsageFree = orig })
	diskUsageFree = func(string) (uint64, bool) { return 0, false }
	dev := lsblkNode{Name: "sda", Type: "disk", Children: []lsblkNode{
		{Name: "sda1", Type: "part", Fstype: "ext4", Mountpoint: "/", Size: 50_000_000_000},
		{Name: "sda2", Type: "part", Fstype: "ext4", Mountpoint: "/data", Size: 450_000_000_000},
	}}
	if mount, _, _ := bestMountOnDisk(dev); mount != "/data" {
		t.Fatalf("expected /data picked for usage, got %q", mount)
	}
	if !diskHasSystemMount(dev) {
		t.Fatal("disk with / must be a system disk")
	}
	data := lsblkNode{Name: "sdb", Type: "disk", Children: []lsblkNode{
		{Name: "sdb1", Type: "part", Fstype: "ext4", Mountpoint: "/data2", Size: 1},
	}}
	if diskHasSystemMount(data) {
		t.Fatal("data-only disk must not be a system disk")
	}
}

func TestIsStorageFstypeSkipsTmpfs(t *testing.T) {
	if isStorageFstype("tmpfs") || !isStorageFstype("ext4") {
		t.Fatal("fstype filter")
	}
}
