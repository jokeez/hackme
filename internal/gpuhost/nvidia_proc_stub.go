//go:build !linux

package gpuhost

// NVIDIAProcCard is a GPU from /proc (Linux-only).
type NVIDIAProcCard struct {
	Index int
	PCI   string
	Name  string
}

// ListNVIDIAProcCards is Linux-only.
func ListNVIDIAProcCards() []NVIDIAProcCard {
	return nil
}
