package gputune

import (
	"encoding/json"
	"errors"
	"strings"
)

// GPUFailureClass categorizes transient hardware/driver faults for worker fallback.
type GPUFailureClass string

const (
	FailureNone    GPUFailureClass = ""
	FailureVRAM    GPUFailureClass = "vram_oom"
	FailureTDR     GPUFailureClass = "driver_tdr"
	FailureThermal GPUFailureClass = "thermal"
	FailureDriver  GPUFailureClass = "driver"
	FailureTimeout GPUFailureClass = "search_timeout"
	FailureUnknown GPUFailureClass = "unknown_gpu"
)

// ClassifyGPUFailure maps accelerator errors to audit classes (chaos matrix).
func ClassifyGPUFailure(err error) GPUFailureClass {
	if err == nil {
		return FailureNone
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "out of memory"), strings.Contains(s, "oom"), strings.Contains(s, "cuda error: 2"),
		strings.Contains(s, "insufficient"), strings.Contains(s, "alloc failed"), strings.Contains(s, "vram"):
		return FailureVRAM
	case strings.Contains(s, "tdr"), strings.Contains(s, "timeout detected"), strings.Contains(s, "display driver"),
		strings.Contains(s, "driver stopped"), strings.Contains(s, "nvlddmkm"), strings.Contains(s, "xid"):
		return FailureTDR
	case strings.Contains(s, "thermal"), strings.Contains(s, "overheat"), strings.Contains(s, "temperature"),
		strings.Contains(s, "temp limit"), strings.Contains(s, "throttl"):
		return FailureThermal
	case strings.Contains(s, "cuda error"), strings.Contains(s, "nvrtc"), strings.Contains(s, "opencl"),
		strings.Contains(s, "cl_build"), strings.Contains(s, "compatnotsupported"), strings.Contains(s, "driver"),
		strings.Contains(s, "invalidcontext"), strings.Contains(s, "invalid context"), strings.Contains(s, "memcpyhtod"):
		return FailureDriver
	case strings.Contains(s, "context deadline"), strings.Contains(s, "search timeout"), strings.Contains(s, "deadline exceeded"):
		return FailureTimeout
	default:
		return FailureUnknown
	}
}

// ShouldCPUFallback reports whether the worker should continue the claim on CPU (no panic).
func ShouldCPUFallback(class GPUFailureClass) bool {
	switch class {
	case FailureVRAM, FailureTDR, FailureThermal, FailureDriver, FailureTimeout, FailureUnknown:
		return true
	default:
		return false
	}
}

// WorkerGPUEvent is a structured stderr/coordinator-adjacent log line (JSON).
type WorkerGPUEvent struct {
	Event     string          `json:"event"`
	WorkerID  string          `json:"worker_id"`
	Backend   string          `json:"backend"`
	Failure   GPUFailureClass `json:"failure_class"`
	Fallback  string          `json:"fallback"`
	Detail    string          `json:"detail,omitempty"`
	SessionOK bool            `json:"session_preserved"`
}

// FormatWorkerGPUEvent renders one JSON line for worker logs (pool operator tail).
func FormatWorkerGPUEvent(workerID, backend string, class GPUFailureClass, err error) string {
	detail := ""
	if err != nil {
		detail = err.Error()
		if len(detail) > 512 {
			detail = detail[:512] + "…"
		}
	}
	ev := WorkerGPUEvent{
		Event:     "gpu_fallback",
		WorkerID:  strings.TrimSpace(workerID),
		Backend:   strings.TrimSpace(backend),
		Failure:   class,
		Fallback:  "cpu_claim_chunk",
		Detail:    detail,
		SessionOK: true,
	}
	b, _ := json.Marshal(ev)
	return string(b)
}

// SimulateChaosFailure returns representative errors for hardware audit tests.
func SimulateChaosFailure(kind string) error {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "vram", "oom":
		return errors.New("CUDA out of memory: allocation failed")
	case "tdr":
		return errors.New("Display driver stopped responding (TDR)")
	case "thermal":
		return errors.New("GPU thermal limit exceeded — throttling")
	case "driver":
		return errors.New("CUDA error: unknown driver error")
	case "timeout":
		return errors.New("context deadline exceeded")
	default:
		return errors.New("accelerator search failed")
	}
}
