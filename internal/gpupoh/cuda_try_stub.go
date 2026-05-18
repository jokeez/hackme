//go:build !cuda

package gpupoh

func tryCUDAAccelerators() ([]Accelerator, error) {
	return nil, nil
}
