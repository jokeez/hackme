//go:build !opencl

package gpupoh

func tryOpenCLAccelerators() ([]Accelerator, error) {
	return nil, nil
}
