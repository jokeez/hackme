package main

import (
	"encoding/json"
	"testing"
)

func TestBestMountOnDiskPicksMostFree(t *testing.T) {
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

func TestIsStorageFstypeSkipsTmpfs(t *testing.T) {
	if isStorageFstype("tmpfs") || !isStorageFstype("ext4") {
		t.Fatal("fstype filter")
	}
}
