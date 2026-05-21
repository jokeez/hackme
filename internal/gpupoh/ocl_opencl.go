//go:build opencl

package gpupoh

/*
#cgo windows LDFLAGS: -lOpenCL
#cgo linux LDFLAGS: -lOpenCL
#cgo darwin LDFLAGS: -framework OpenCL

#define CL_TARGET_OPENCL_VERSION 120
#ifdef __APPLE__
#include <OpenCL/opencl.h>
#else
#include <CL/cl.h>
#endif

#include <stdlib.h>
#include <string.h>

static cl_program poh_build_program(cl_context ctx, cl_device_id dev, const char *src, char **log_out) {
	cl_int err = 0;
	size_t srclen = strlen(src);
	cl_program p = clCreateProgramWithSource(ctx, 1, &src, &srclen, &err);
	if (err != CL_SUCCESS || !p) {
		return NULL;
	}
	const char *opts = "-cl-std=CL1.2";
	err = clBuildProgram(p, 1, &dev, opts, NULL, NULL);
	if (err != CL_SUCCESS) {
		size_t log_size = 0;
		clGetProgramBuildInfo(p, dev, CL_PROGRAM_BUILD_LOG, 0, NULL, &log_size);
		if (log_size > 0) {
			char *buf = (char *)malloc(log_size + 1);
			if (buf) {
				clGetProgramBuildInfo(p, dev, CL_PROGRAM_BUILD_LOG, log_size, buf, NULL);
				buf[log_size] = 0;
				*log_out = buf;
			}
		}
		clReleaseProgram(p);
		return NULL;
	}
	return p;
}

static cl_kernel poh_create_kernel(cl_program p) {
	cl_int err = 0;
	return clCreateKernel(p, "poh_search", &err);
}
*/
import "C"

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unsafe"

	"github.com/pkg/errors"
)

const oclKernelSource = `
// No cl_khr_int64_base_atomics: Mesa rusticl rejects ulong atom_cmpxchg; use int spinlock + plain ulong writes.
__kernel void poh_search(
	const ulong base,
	const ulong count,
	const ulong mod,
	__global ulong *out_nonce,
	volatile __global int *lock
) {
	size_t tid = get_global_id(0);
	if (tid >= (size_t)count) {
		return;
	}
	ulong n = base + (ulong)tid;
	ulong v = n * 7UL + 13UL;
	if (mod == 0UL) {
		return;
	}
	if ((v % mod) != 0UL) {
		return;
	}
	while (atomic_xchg(lock, 1) != 0) { }
	ulong cur = *out_nonce;
	if (n < cur) {
		*out_nonce = n;
	}
	atomic_xchg(lock, 0);
}
`

type oclAccel struct {
	devID   int
	devName string
	ctx     C.cl_context
	queue   C.cl_command_queue
	program C.cl_program
	kernel  C.cl_kernel
	memOut  C.cl_mem
	memLock C.cl_mem
}

func (a *oclAccel) DeviceIndex() int   { return a.devID }
func (a *oclAccel) DeviceName() string { return a.devName }
func (a *oclAccel) Backend() string    { return "opencl" }
func (a *oclAccel) Label() string      { return fmt.Sprintf("GPU #%d [OpenCL]", a.devID) }
func (a *oclAccel) Close() error       { return a.close() }

func (a *oclAccel) close() error {
	if a.memLock != nil {
		_ = C.clReleaseMemObject(a.memLock)
		a.memLock = nil
	}
	if a.memOut != nil {
		_ = C.clReleaseMemObject(a.memOut)
		a.memOut = nil
	}
	if a.kernel != nil {
		_ = C.clReleaseKernel(a.kernel)
		a.kernel = nil
	}
	if a.program != nil {
		_ = C.clReleaseProgram(a.program)
		a.program = nil
	}
	if a.queue != nil {
		_ = C.clReleaseCommandQueue(a.queue)
		a.queue = nil
	}
	if a.ctx != nil {
		_ = C.clReleaseContext(a.ctx)
		a.ctx = nil
	}
	return nil
}

func (a *oclAccel) Search(ctx context.Context, base, count, mod uint64) (found bool, nonce uint64, err error) {
	if count == 0 || mod == 0 {
		return false, 0, nil
	}
	if count > maxBatch {
		count = maxBatch
	}
	select {
	case <-ctx.Done():
		return false, 0, ctx.Err()
	default:
	}
	t0 := time.Now()

	var out uint64 = ^uint64(0)
	errN := C.clEnqueueWriteBuffer(a.queue, a.memOut, C.CL_TRUE, 0, C.size_t(8), unsafe.Pointer(&out), 0, nil, nil)
	if errN != C.CL_SUCCESS {
		return false, 0, fmt.Errorf("gpupoh ocl: EnqueueWriteBuffer init %d", int(errN))
	}
	var lockZero C.cl_int
	errN = C.clEnqueueWriteBuffer(a.queue, a.memLock, C.CL_TRUE, 0, C.size_t(4), unsafe.Pointer(&lockZero), 0, nil, nil)
	if errN != C.CL_SUCCESS {
		return false, 0, fmt.Errorf("gpupoh ocl: EnqueueWriteBuffer lock %d", int(errN))
	}

	b := C.cl_ulong(base)
	c := C.cl_ulong(count)
	m := C.cl_ulong(mod)
	errN = C.clSetKernelArg(a.kernel, 0, C.size_t(unsafe.Sizeof(b)), unsafe.Pointer(&b))
	if errN != C.CL_SUCCESS {
		return false, 0, fmt.Errorf("gpupoh ocl: SetKernelArg0 %d", int(errN))
	}
	errN = C.clSetKernelArg(a.kernel, 1, C.size_t(unsafe.Sizeof(c)), unsafe.Pointer(&c))
	if errN != C.CL_SUCCESS {
		return false, 0, fmt.Errorf("gpupoh ocl: SetKernelArg1 %d", int(errN))
	}
	errN = C.clSetKernelArg(a.kernel, 2, C.size_t(unsafe.Sizeof(m)), unsafe.Pointer(&m))
	if errN != C.CL_SUCCESS {
		return false, 0, fmt.Errorf("gpupoh ocl: SetKernelArg2 %d", int(errN))
	}
	errN = C.clSetKernelArg(a.kernel, 3, C.size_t(unsafe.Sizeof(a.memOut)), unsafe.Pointer(&a.memOut))
	if errN != C.CL_SUCCESS {
		return false, 0, fmt.Errorf("gpupoh ocl: SetKernelArg3 %d", int(errN))
	}
	errN = C.clSetKernelArg(a.kernel, 4, C.size_t(unsafe.Sizeof(a.memLock)), unsafe.Pointer(&a.memLock))
	if errN != C.CL_SUCCESS {
		return false, 0, fmt.Errorf("gpupoh ocl: SetKernelArg4 %d", int(errN))
	}

	global := C.size_t(count)
	errN = C.clEnqueueNDRangeKernel(a.queue, a.kernel, 1, nil, &global, nil, 0, nil, nil)
	if errN != C.CL_SUCCESS {
		return false, 0, fmt.Errorf("gpupoh ocl: NDRangeKernel %d", int(errN))
	}
	errN = C.clFinish(a.queue)
	if errN != C.CL_SUCCESS {
		return false, 0, fmt.Errorf("gpupoh ocl: Finish %d", int(errN))
	}
	kernelSec := time.Since(t0).Seconds()
	recordOCLKernelDuration(kernelSec)
	if os.Getenv("HACKME_OPENCL_VERBOSE") == "1" {
		ghs := float64(count) / kernelSec / 1e9
		fmt.Fprintf(os.Stderr, "gpupoh: opencl search count=%d elapsed=%s ~%.2f GH/s\n",
			count, time.Duration(kernelSec*float64(time.Second)).Round(time.Millisecond), ghs)
	}

	errN = C.clEnqueueReadBuffer(a.queue, a.memOut, C.CL_TRUE, 0, C.size_t(8), unsafe.Pointer(&out), 0, nil, nil)
	if errN != C.CL_SUCCESS {
		return false, 0, fmt.Errorf("gpupoh ocl: ReadBuffer %d", int(errN))
	}
	maxU := ^uint64(0)
	if out == maxU {
		return false, 0, nil
	}
	return true, out, nil
}

func oclDeviceName(dev C.cl_device_id) string {
	var buf [512]C.char
	var ret C.size_t
	if C.clGetDeviceInfo(dev, C.CL_DEVICE_NAME, C.size_t(len(buf)), unsafe.Pointer(&buf[0]), &ret) != C.CL_SUCCESS {
		return "OpenCL device"
	}
	return C.GoString(&buf[0])
}

func newOCLAccelerator(globalIndex int, dev C.cl_device_id) (Accelerator, error) {
	var err C.cl_int
	ctx := C.clCreateContext(nil, 1, &dev, nil, nil, &err)
	if ctx == nil || err != C.CL_SUCCESS {
		return nil, fmt.Errorf("gpupoh ocl: CreateContext %d", int(err))
	}
	queue := C.clCreateCommandQueue(ctx, dev, 0, &err)
	if queue == nil || err != C.CL_SUCCESS {
		_ = C.clReleaseContext(ctx)
		return nil, fmt.Errorf("gpupoh ocl: CreateCommandQueue %d", int(err))
	}
	csrc := C.CString(oclKernelSource)
	defer C.free(unsafe.Pointer(csrc))
	var buildLog *C.char
	prog := C.poh_build_program(ctx, dev, csrc, &buildLog)
	if buildLog != nil {
		defer C.free(unsafe.Pointer(buildLog))
	}
	if prog == nil {
		_ = C.clReleaseCommandQueue(queue)
		_ = C.clReleaseContext(ctx)
		msg := "gpupoh ocl: program build failed"
		if buildLog != nil {
			msg += ": " + C.GoString(buildLog)
		}
		return nil, errors.New(msg)
	}
	kern := C.poh_create_kernel(prog)
	if kern == nil {
		_ = C.clReleaseProgram(prog)
		_ = C.clReleaseCommandQueue(queue)
		_ = C.clReleaseContext(ctx)
		return nil, errors.New("gpupoh ocl: CreateKernel poh_search")
	}
	memOut := C.clCreateBuffer(ctx, C.CL_MEM_READ_WRITE, C.size_t(8), nil, &err)
	if memOut == nil || err != C.CL_SUCCESS {
		_ = C.clReleaseKernel(kern)
		_ = C.clReleaseProgram(prog)
		_ = C.clReleaseCommandQueue(queue)
		_ = C.clReleaseContext(ctx)
		return nil, fmt.Errorf("gpupoh ocl: CreateBuffer out %d", int(err))
	}
	memLock := C.clCreateBuffer(ctx, C.CL_MEM_READ_WRITE, C.size_t(4), nil, &err)
	if memLock == nil || err != C.CL_SUCCESS {
		_ = C.clReleaseMemObject(memOut)
		_ = C.clReleaseKernel(kern)
		_ = C.clReleaseProgram(prog)
		_ = C.clReleaseCommandQueue(queue)
		_ = C.clReleaseContext(ctx)
		return nil, fmt.Errorf("gpupoh ocl: CreateBuffer lock %d", int(err))
	}
	name := strings.TrimSpace(oclDeviceName(dev))
	if name == "" {
		name = fmt.Sprintf("OpenCL device %d", globalIndex)
	}
	return &oclAccel{
		devID:   globalIndex,
		devName: name,
		ctx:     ctx,
		queue:   queue,
		program: prog,
		kernel:  kern,
		memOut:  memOut,
		memLock: memLock,
	}, nil
}

// oclListGPUClDevices returns GPU-class OpenCL device IDs from all platforms (stable order).
func oclListGPUClDevices() []C.cl_device_id {
	var nplat C.cl_uint
	if C.clGetPlatformIDs(0, nil, &nplat) != C.CL_SUCCESS || nplat == 0 {
		return nil
	}
	plats := make([]C.cl_platform_id, nplat)
	if C.clGetPlatformIDs(nplat, &plats[0], nil) != C.CL_SUCCESS {
		return nil
	}
	var devices []C.cl_device_id
	for _, plat := range plats {
		var ndev C.cl_uint
		if C.clGetDeviceIDs(plat, C.CL_DEVICE_TYPE_GPU|C.CL_DEVICE_TYPE_ACCELERATOR, 0, nil, &ndev) != C.CL_SUCCESS || ndev == 0 {
			continue
		}
		devs := make([]C.cl_device_id, ndev)
		if C.clGetDeviceIDs(plat, C.CL_DEVICE_TYPE_GPU|C.CL_DEVICE_TYPE_ACCELERATOR, ndev, &devs[0], nil) != C.CL_SUCCESS {
			continue
		}
		for _, d := range devs {
			devices = append(devices, d)
		}
	}
	return devices
}

// OpenCLAcceleratorInitDiagnostics runs a full kernel build per GPU device (same as mining init).
// Use for operator probes when DiscoverAccelerators returns empty (driver / rusticl issues).
func OpenCLAcceleratorInitDiagnostics() []string {
	devices := oclListGPUClDevices()
	var lines []string
	for i, dev := range devices {
		name := strings.TrimSpace(oclDeviceName(dev))
		if name == "" {
			name = fmt.Sprintf("OpenCL device %d", i)
		}
		acc, err := newOCLAccelerator(i, dev)
		if err != nil {
			lines = append(lines, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		_ = acc.Close()
		lines = append(lines, fmt.Sprintf("%s: kernel init ok", name))
	}
	return lines
}

// tryOpenCLAccelerators enumerates GPU-class devices on all platforms.
func tryOpenCLAccelerators() ([]Accelerator, error) {
	devices := oclListGPUClDevices()
	if len(devices) == 0 {
		return nil, nil
	}
	var out []Accelerator
	for i, dev := range devices {
		a, err := newOCLAccelerator(i, dev)
		if err != nil {
			// Some OpenCL devices/drivers are flaky; skip failed ones
			// and keep the devices that can actually execute kernels.
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// listOpenCLGPUDevices returns GPU-class OpenCL devices (names only).
func listOpenCLGPUDevices() []GPUDeviceInfo {
	devices := oclListGPUClDevices()
	out := make([]GPUDeviceInfo, 0, len(devices))
	for i, dev := range devices {
		name := strings.TrimSpace(oclDeviceName(dev))
		if name == "" {
			name = fmt.Sprintf("OpenCL device %d", i)
		}
		out = append(out, GPUDeviceInfo{Index: i, Name: name, Backend: "opencl"})
	}
	return out
}
