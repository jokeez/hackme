package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"hackme/internal/hunt"
	"hackme/internal/poolsync"
)

// huntOneShotRequest is POST /api/hunt/campaigns/one-shot — publish local harness
// then create the campaign as one resilient flow (fail before escrow on size/sync).
type huntOneShotRequest struct {
	hunt.CreateRequest
	BinaryPath string `json:"binary_path"`
	SourceRel  string `json:"source_rel,omitempty"`
}

func writeHarnessSyncAPIError(w http.ResponseWriter, err error, extra map[string]any) {
	code, status, msg := poolsync.ClassifyHarnessSyncError(err)
	if extra == nil {
		extra = map[string]any{}
	}
	extra["detail"] = err.Error()
	writeAPIError(w, status, code, msg, extra)
}

func (a *app) handleHuntCampaignOneShot(w http.ResponseWriter, r *http.Request) {
	if !requireFuzzCampaignCreateAuth(w, r) {
		return
	}
	if hasValidAdminAuth(r) {
		logAdminAction(r, "hunt_campaign_one_shot")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	defer r.Body.Close()
	var req huntOneShotRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeAPIError(w, http.StatusRequestEntityTooLarge, poolsync.CodeHarnessTooLarge,
				"request body too large", nil)
			return
		}
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid json", nil)
		return
	}
	binPath := strings.TrimSpace(req.BinaryPath)
	if binPath == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request",
			"binary_path required for one-shot publish+create", nil)
		return
	}
	req.PoolDistributed = true
	hash := strings.TrimSpace(strings.ToLower(req.HarnessHash))
	if hash == "" {
		base := filepath.Base(binPath)
		base = strings.TrimSuffix(base, filepath.Ext(base))
		if hunt.ValidHarnessHash(base) {
			hash = strings.ToLower(base)
			req.HarnessHash = hash
		}
	}
	if hash == "" || !hunt.ValidHarnessHash(hash) {
		writeAPIError(w, http.StatusBadRequest, "invalid_request",
			"harness_hash required (or binary_path basename must be a harness hash)", nil)
		return
	}
	sourceRel := strings.TrimSpace(req.SourceRel)
	if sourceRel == "" && req.Inventory != nil {
		sourceRel = strings.TrimSpace(req.Inventory.Path)
	}

	root := a.repoRoot()
	size, absPath, err := hunt.PreflightHarnessBinarySize(root, binPath)
	if err != nil {
		writeHarnessSyncAPIError(w, err, map[string]any{
			"harness_hash": hash,
			"binary_path":  binPath,
			"byte_size":    size,
			"max_bytes":    hunt.CoordinatorHarnessUploadMaxBytes,
		})
		return
	}
	if err := hunt.PublishHarnessFile(r.Context(), a.db, hash, absPath, sourceRel); err != nil {
		writeHarnessSyncAPIError(w, err, map[string]any{
			"harness_hash": hash,
			"binary_path":  absPath,
		})
		return
	}
	contentSHA, cerr := hunt.GetHarnessContentSHA256(r.Context(), a.db, hash)
	if cerr != nil {
		writeAPIError(w, http.StatusBadRequest, "publish_failed", cerr.Error(), map[string]any{
			"harness_hash": hash,
		})
		return
	}
	req.HarnessContentSHA256 = contentSHA
	req.HarnessHash = hash

	cfgProbe := map[string]any{
		"harness_hash":    hash,
		"hunt_source_rel": sourceRel,
	}
	if syncErr := a.syncHuntHarnessToCoordinator(r.Context(), cfgProbe); syncErr != nil {
		writeHarnessSyncAPIError(w, syncErr, map[string]any{
			"harness_hash":           hash,
			"harness_content_sha256": contentSHA,
			"byte_size":              size,
		})
		return
	}

	createBody, err := json.Marshal(req.CreateRequest)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "encode_failed", "failed to encode create request", nil)
		return
	}
	createReq := r.Clone(r.Context())
	createReq.Method = http.MethodPost
	createReq.Body = io.NopCloser(bytes.NewReader(createBody))
	createReq.ContentLength = int64(len(createBody))
	createReq.Header = r.Header.Clone()
	createReq.Header.Set("Content-Type", "application/json")
	a.handleHuntCampaignCreate(w, createReq)
}
