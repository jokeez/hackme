//go:build cuda

package gpupoh

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/pkg/errors"
	"gorgonia.org/cu"
	"gorgonia.org/cu/nvrtc"
)

//go:embed poh_search.cu
var pohCuSource string

const blockThreads = 256

var pohPTX struct {
	once  sync.Once
	ptx   []byte
	kname string
	err   error
}

func compilePTXForPoH() (ptx []byte, kname string, err error) {
	pohPTX.once.Do(func() {
		prog, e := nvrtc.CreateProgram(pohCuSource, "poh_search.cu")
		if e != nil {
			pohPTX.err = errors.Wrap(e, "gpupoh: nvrtc CreateProgram")
			return
		}
		defer func() { _ = prog.Destroy() }()
		if e := prog.AddNameExpression("poh_search"); e != nil {
			pohPTX.err = errors.Wrap(e, "gpupoh: AddNameExpression")
			return
		}
		opts := []string{"--gpu-architecture=compute_60"}
		if e := prog.Compile(opts...); e != nil {
			log, _ := prog.GetLog()
			pohPTX.err = fmt.Errorf("gpupoh: nvrtc Compile: %w (log: %s)", e, strings.TrimSpace(log))
			return
		}
		ptxBytes, e := prog.GetPTX()
		if e != nil {
			pohPTX.err = errors.Wrap(e, "gpupoh: GetPTX")
			return
		}
		kn, e := prog.GetLoweredName("poh_search")
		if e != nil {
			pohPTX.err = errors.Wrap(e, "gpupoh: GetLoweredName")
			return
		}
		pohPTX.ptx = ptxBytes
		pohPTX.kname = kn
	})
	return pohPTX.ptx, pohPTX.kname, pohPTX.err
}

type cudaAccel struct {
	devID   int
	devName string
	ctx     cu.CUContext
	mod     cu.Module
	kernel  cu.Function
	dOut    cu.DevicePtr

	haveCtx bool
	haveMod bool
}

func newCUDAAccelerator(devID int, ptx []byte, kname string) (Accelerator, error) {
	dev, err := cu.GetDevice(devID)
	if err != nil {
		return nil, errors.Wrap(err, "gpupoh: GetDevice")
	}
	name, _ := dev.Name()
	if name == "" {
		name = fmt.Sprintf("CUDA device %d", devID)
	}
	ctx, err := dev.MakeContext(cu.SchedAuto)
	if err != nil {
		return nil, errors.Wrap(err, "gpupoh: MakeContext")
	}
	mod, err := cu.LoadData(ptx)
	if err != nil {
		_ = ctx.Destroy()
		return nil, errors.Wrap(err, "gpupoh: LoadData")
	}
	fn, err := mod.Function(kname)
	if err != nil {
		_ = mod.Unload()
		_ = ctx.Destroy()
		return nil, errors.Wrap(err, "gpupoh: Function")
	}
	dOut, err := cu.MemAlloc(8)
	if err != nil {
		_ = mod.Unload()
		_ = ctx.Destroy()
		return nil, errors.Wrap(err, "gpupoh: MemAlloc")
	}
	return &cudaAccel{
		devID:   devID,
		devName: name,
		ctx:     ctx,
		mod:     mod,
		kernel:  fn,
		dOut:    dOut,
		haveCtx: true,
		haveMod: true,
	}, nil
}

func (a *cudaAccel) DeviceIndex() int   { return a.devID }
func (a *cudaAccel) DeviceName() string { return a.devName }
func (a *cudaAccel) Backend() string    { return "cuda" }
func (a *cudaAccel) Label() string      { return fmt.Sprintf("GPU #%d [CUDA]", a.devID) }
func (a *cudaAccel) Close() error       { return a.close() }

func (a *cudaAccel) close() error {
	var first error
	if a.dOut != 0 {
		if err := cu.MemFree(a.dOut); err != nil && first == nil {
			first = err
		}
		a.dOut = 0
	}
	if a.haveMod {
		if err := a.mod.Unload(); err != nil && first == nil {
			first = err
		}
		a.haveMod = false
	}
	if a.haveCtx {
		if err := a.ctx.Destroy(); err != nil && first == nil {
			first = err
		}
		a.haveCtx = false
	}
	return first
}

func (a *cudaAccel) Search(ctx context.Context, base, count, mod uint64) (found bool, nonce uint64, err error) {
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

	maxU := ^uint64(0)
	if err := cu.MemcpyHtoD(a.dOut, unsafe.Pointer(&maxU), 8); err != nil {
		return false, 0, errors.Wrap(err, "gpupoh: MemcpyHtoD init")
	}

	b := base
	c := count
	m := mod
	dptr := a.dOut
	args := []unsafe.Pointer{
		unsafe.Pointer(&b),
		unsafe.Pointer(&c),
		unsafe.Pointer(&m),
		unsafe.Pointer(&dptr),
	}

	grid := cu.DivUp(int(count), blockThreads)
	if grid < 1 {
		grid = 1
	}

	if err := a.kernel.Launch(grid, 1, 1, blockThreads, 1, 1, 0, cu.Stream{}, args); err != nil {
		return false, 0, errors.Wrap(err, "gpupoh: Launch")
	}
	if err := cu.Synchronize(); err != nil {
		return false, 0, errors.Wrap(err, "gpupoh: Synchronize")
	}

	var out uint64
	if err := cu.MemcpyDtoH(unsafe.Pointer(&out), a.dOut, 8); err != nil {
		return false, 0, errors.Wrap(err, "gpupoh: MemcpyDtoH")
	}
	if out == maxU {
		return false, 0, nil
	}
	return true, out, nil
}
