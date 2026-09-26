package router_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/blueship581/print-color-calibration-release/backend/internal/config"
	"github.com/blueship581/print-color-calibration-release/backend/internal/database"
	"github.com/blueship581/print-color-calibration-release/backend/internal/router"
	"github.com/gin-gonic/gin"
)

type proofRef struct {
	ID      uint   `json:"id"`
	Version uint   `json:"version"`
	Status  string `json:"status"`
}

type runRef struct {
	ID         uint    `json:"id"`
	Version    uint    `json:"version"`
	Status     string  `json:"status"`
	HoldReason string  `json:"holdReason"`
	Tolerance  float64 `json:"colorTolerance"`
}

// TestThreePositionProofGate exercises the end-to-end HTTP flow:
//   - a proof missing one position cannot be submitted for review,
//   - a complete proof whose worst position exceeds the batch tolerance
//     cannot be accepted and holds the linked batch,
//   - re-measurement creates a new judgment version while the old
//     conclusion remains in history.
func TestThreePositionProofGate(t *testing.T) {
	cfg := testConfig(filepath.Join(t.TempDir(), "gate-router.db"))
	engine := newTestEngine(t, cfg)
	operator := loginToken(t, engine, "operator")
	reviewer := loginToken(t, engine, "reviewer")

	// Batch with a 3.0 ΔE tolerance moved into proofing.
	runCode := "PR-GATE-001"
	status, body := perform(t, engine, http.MethodPost, "/api/runs", operator, "gate-run-create",
		proofPayload(runCode, "门控批次", map[string]any{"colorTolerance": 3.0}))
	if status != http.StatusCreated {
		t.Fatalf("create run status=%d body=%s", status, body)
	}
	run := decodeData[runRef](t, body)
	if run.Tolerance != 3.0 {
		t.Fatalf("batch tolerance = %.2f, want 3.0", run.Tolerance)
	}
	runVersion := func() uint {
		t.Helper()
		_, detail := perform(t, engine, http.MethodGet, "/api/runs/"+uintString(run.ID), operator, "gate-run-version", nil)
		return decodeData[runRef](t, detail).Version
	}
	moveRun := func(to, requestID, reason string) {
		t.Helper()
		if code, _ := perform(t, engine, http.MethodPost, "/api/runs/"+uintString(run.ID)+"/transition", operator, requestID,
			map[string]any{"status": to, "expectedVersion": runVersion(), "reason": reason}); code != http.StatusOK {
			t.Fatalf("run -> %s status = %d", to, code)
		}
	}
	moveRun("printing", "gate-run-printing", "plates verified")
	moveRun("proofing", "gate-run-proofing", "proof strip ready")

	// Proof with the drive-side reading missing.
	proofCode := "CP-GATE-001"
	status, body = perform(t, engine, http.MethodPost, "/api/proofs", operator, "gate-proof-create",
		proofPayload(proofCode, "缺录位置校样", map[string]any{
			"relatedCode": runCode, "operationSide": 1.2, "center": 1.5,
		}))
	if status != http.StatusCreated {
		t.Fatalf("create proof status = %d body=%s", status, body)
	}
	proof := decodeData[proofRef](t, body)
	if status, _ := perform(t, engine, http.MethodPost, "/api/proofs/"+uintString(proof.ID)+"/transition", operator, "gate-proof-missing",
		map[string]any{"status": "review", "expectedVersion": proof.Version, "reason": "missing drive position"}); status != http.StatusUnprocessableEntity {
		t.Fatalf("missing-position submit status = %d, want 422", status)
	}
	// Batch is held at proofing with the missing-reading reason.
	status, body = perform(t, engine, http.MethodGet, "/api/runs/"+uintString(run.ID), operator, "gate-run-held-missing", nil)
	held := decodeData[runRef](t, body)
	if status != http.StatusOK || held.Status != "hold" || held.HoldReason == "" {
		t.Fatalf("run should be held for missing reading: %+v", held)
	}

	// Complete readings, but the drive side exceeds the batch tolerance.
	status, body = perform(t, engine, http.MethodPut, "/api/proofs/"+uintString(proof.ID), operator, "gate-proof-complete",
		proofPayload("ignored", "缺录位置校样", map[string]any{
			"expectedVersion": proof.Version, "relatedCode": runCode,
			"operationSide": 1.2, "center": 1.5, "driveSide": 3.9,
		}))
	if status != http.StatusOK {
		t.Fatalf("complete readings status = %d body=%s", status, body)
	}
	proof = decodeData[proofRef](t, body)
	if status, _ = perform(t, engine, http.MethodPost, "/api/proofs/"+uintString(proof.ID)+"/transition", operator, "gate-proof-review",
		map[string]any{"status": "review", "expectedVersion": proof.Version, "reason": "three positions captured"}); status != http.StatusOK {
		t.Fatalf("submit review status = %d", status)
	}
	_, body = perform(t, engine, http.MethodGet, "/api/proofs/"+uintString(proof.ID), operator, "gate-proof-read-review", nil)
	proof = decodeData[proofRef](t, body)
	if status, _ := perform(t, engine, http.MethodPost, "/api/proofs/"+uintString(proof.ID)+"/transition", reviewer, "gate-proof-accept-over",
		map[string]any{"status": "accepted", "expectedVersion": proof.Version, "reason": "worst drive side over limit"}); status != http.StatusUnprocessableEntity {
		t.Fatalf("over-tolerance accept status = %d, want 422", status)
	}
	_, body = perform(t, engine, http.MethodGet, "/api/runs/"+uintString(run.ID), reviewer, "gate-run-held-over", nil)
	over := decodeData[runRef](t, body)
	if over.Status != "hold" || over.HoldReason == "" {
		t.Fatalf("run should be held for over-tolerance proof: %+v", over)
	}

	// Re-measure within tolerance; a new judgment version supersedes v1.
	status, body = perform(t, engine, http.MethodPut, "/api/proofs/"+uintString(proof.ID), operator, "gate-proof-reread",
		proofPayload("ignored", "重新测量校样", map[string]any{
			"expectedVersion": proof.Version, "relatedCode": runCode,
			"operationSide": 1.1, "center": 1.3, "driveSide": 1.7,
		}))
	if status != http.StatusOK {
		t.Fatalf("re-measure status = %d body=%s", status, body)
	}
	proof = decodeData[proofRef](t, body)
	if proof.Status != "captured" {
		t.Fatalf("re-measured proof status = %s, want captured", proof.Status)
	}
	if status, _ = perform(t, engine, http.MethodPost, "/api/proofs/"+uintString(proof.ID)+"/transition", operator, "gate-proof-review-2",
		map[string]any{"status": "review", "expectedVersion": proof.Version, "reason": "re-measured"}); status != http.StatusOK {
		t.Fatalf("second review submit = %d", status)
	}
	_, body = perform(t, engine, http.MethodGet, "/api/proofs/"+uintString(proof.ID), operator, "gate-proof-read-review-2", nil)
	proof = decodeData[proofRef](t, body)
	if status, _ = perform(t, engine, http.MethodPost, "/api/proofs/"+uintString(proof.ID)+"/transition", reviewer, "gate-proof-accept-2",
		map[string]any{"status": "accepted", "expectedVersion": proof.Version, "reason": "worst position now within limit"}); status != http.StatusOK {
		t.Fatalf("second accept status = %d", status)
	}

	// Detail shows the active v3 judgment accepted and the older pending
	// rounds (incomplete capture, over-tolerance measurement) left in history.
	_, body = perform(t, engine, http.MethodGet, "/api/proofs/"+uintString(proof.ID), reviewer, "gate-proof-detail", nil)
	detail := decodeData[struct {
		WorstPosition string  `json:"worstPosition"`
		MetricValue   float64 `json:"metricValue"`
		Judgments     []struct {
			JudgmentNo uint    `json:"judgmentNo"`
			Active     bool    `json:"active"`
			Conclusion string  `json:"conclusion"`
			WorstValue float64 `json:"worstValue"`
		} `json:"judgments"`
	}](t, body)
	if detail.WorstPosition != "drive" || detail.MetricValue != 1.7 {
		t.Fatalf("detail worst mismatch: %+v", detail)
	}
	if len(detail.Judgments) != 3 || !detail.Judgments[0].Active || detail.Judgments[0].JudgmentNo != 3 ||
		detail.Judgments[0].Conclusion != "accepted" ||
		detail.Judgments[1].JudgmentNo != 2 || detail.Judgments[1].WorstValue != 3.9 ||
		detail.Judgments[2].JudgmentNo != 1 || detail.Judgments[2].WorstValue != 1.5 {
		t.Fatalf("judgment history mismatch: %+v", detail.Judgments)
	}
	// Batch gate cleared after successful re-review.
	_, body = perform(t, engine, http.MethodGet, "/api/runs/"+uintString(run.ID), reviewer, "gate-run-cleared", nil)
	cleared := decodeData[runRef](t, body)
	if cleared.Status != "proofing" || cleared.HoldReason != "" {
		t.Fatalf("batch gate should be cleared: %+v", cleared)
	}
}

// proofPayload builds a valid write payload and overlays extra fields.
func proofPayload(code, name string, extra map[string]any) map[string]any {
	payload := map[string]any{
		"code": code, "name": name, "description": "three position gate test",
		"facility": "测试印刷区", "owner": "operator", "category": "校准",
		"riskLevel": "medium", "metricUnit": "ΔE",
		"effectiveAt": time.Now().UTC().Format(time.RFC3339), "evidence": "spectrophotometer evidence",
	}
	for key, value := range extra {
		payload[key] = value
	}
	return payload
}

func newTestEngine(t *testing.T, cfg config.Config) *gin.Engine {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, redisClient, err := database.Open(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	return router.New(cfg, db, redisClient, logger)
}
