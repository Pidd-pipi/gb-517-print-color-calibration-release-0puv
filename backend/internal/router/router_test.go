package router_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/blueship581/print-color-calibration-release/backend/internal/config"
	"github.com/blueship581/print-color-calibration-release/backend/internal/database"
	"github.com/blueship581/print-color-calibration-release/backend/internal/router"
	"github.com/gin-gonic/gin"
)

type apiEnvelope struct {
	Data json.RawMessage `json:"data"`
}

func TestRBACAndImmutableRevisionFlows(t *testing.T) {
	cfg := testConfig(filepath.Join(t.TempDir(), "gb517.db"))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, redisClient, err := database.Open(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	engine := router.New(cfg, db, redisClient, logger)
	tokens := map[string]string{}
	for _, role := range []string{"viewer", "operator", "reviewer", "admin"} {
		tokens[role] = loginToken(t, engine, role)
	}

	payload := recordPayload("RD-TEST-001", "测试放行决定")
	if status, _ := perform(t, engine, http.MethodPost, "/api/release", tokens["viewer"], "viewer-create", payload); status != http.StatusForbidden {
		t.Fatalf("viewer create status = %d, want 403", status)
	}
	status, body := perform(t, engine, http.MethodPost, "/api/release", tokens["operator"], "decision-create", payload)
	if status != http.StatusCreated {
		t.Fatalf("operator create decision status = %d body=%s", status, body)
	}
	decision := decodeData[struct {
		ID      uint `json:"id"`
		Version uint `json:"version"`
	}](t, body)
	transition := map[string]any{"status": "release", "expectedVersion": decision.Version, "reason": "quality gate accepted"}
	path := "/api/release/" + uintString(decision.ID) + "/transition"
	if status, _ := perform(t, engine, http.MethodPost, path, tokens["operator"], "operator-release", transition); status != http.StatusForbidden {
		t.Fatalf("operator release status = %d, want 403", status)
	}
	status, body = perform(t, engine, http.MethodPost, path, tokens["reviewer"], "reviewer-release", transition)
	if status != http.StatusOK {
		t.Fatalf("reviewer release status = %d body=%s", status, body)
	}
	status, body = perform(t, engine, http.MethodGet, "/api/release/"+uintString(decision.ID), tokens["reviewer"], "decision-read", nil)
	detail := decodeData[struct {
		Version   uint `json:"version"`
		Revisions []struct {
			Version   uint   `json:"version"`
			RequestID string `json:"requestId"`
		} `json:"revisions"`
	}](t, body)
	if status != http.StatusOK || detail.Version != 2 || len(detail.Revisions) != 2 || detail.Revisions[0].RequestID != "reviewer-release" {
		t.Fatalf("unexpected decision revision chain: status=%d detail=%+v", status, detail)
	}
	update := recordPayload("ignored", "不得覆盖的决定")
	update["expectedVersion"] = detail.Version
	if status, _ := perform(t, engine, http.MethodPut, "/api/release/"+uintString(decision.ID), tokens["operator"], "locked-update", update); status != http.StatusConflict {
		t.Fatalf("resolved decision update status = %d, want 409", status)
	}
	if status, _ := perform(t, engine, http.MethodDelete, "/api/release/"+uintString(decision.ID), tokens["admin"], "locked-delete", nil); status != http.StatusConflict {
		t.Fatalf("resolved decision delete status = %d, want 409", status)
	}

	runPayload := recordPayload("PR-TEST-001", "测试色彩配置")
	status, body = perform(t, engine, http.MethodPost, "/api/runs", tokens["operator"], "run-create", runPayload)
	run := decodeData[struct {
		ID      uint `json:"id"`
		Version uint `json:"version"`
	}](t, body)
	if status != http.StatusCreated {
		t.Fatalf("create run status = %d body=%s", status, body)
	}
	runPath := "/api/runs/" + uintString(run.ID) + "/transition"
	status, _ = perform(t, engine, http.MethodPost, runPath, tokens["operator"], "run-printing", map[string]any{"status": "printing", "expectedVersion": run.Version, "reason": "plates and ink verified"})
	if status != http.StatusOK {
		t.Fatalf("run transition status = %d", status)
	}
	if status, _ := perform(t, engine, http.MethodDelete, "/api/runs/"+uintString(run.ID), tokens["admin"], "locked-run-delete", nil); status != http.StatusConflict {
		t.Fatalf("active run delete status = %d, want 409", status)
	}
	_, body = perform(t, engine, http.MethodGet, "/api/runs/"+uintString(run.ID), tokens["operator"], "run-read", nil)
	runDetail := decodeData[struct {
		Revisions []struct {
			RequestID string `json:"requestId"`
		} `json:"revisions"`
	}](t, body)
	if len(runDetail.Revisions) != 2 || runDetail.Revisions[0].RequestID != "run-printing" {
		t.Fatalf("unexpected colour configuration revisions: %+v", runDetail.Revisions)
	}

	if status, _ := perform(t, engine, http.MethodGet, "/api/audits", tokens["viewer"], "viewer-audit", nil); status != http.StatusForbidden {
		t.Fatalf("viewer audit status = %d, want 403", status)
	}
	if status, _ := perform(t, engine, http.MethodGet, "/api/audits", tokens["reviewer"], "reviewer-audit", nil); status != http.StatusOK {
		t.Fatalf("reviewer audit status = %d, want 200", status)
	}
}

func TestProofGateAndJudgmentVersions(t *testing.T) {
	cfg := testConfig(filepath.Join(t.TempDir(), "gb517-gate.db"))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, redisClient, err := database.Open(context.Background(), cfg, logger)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	engine := router.New(cfg, db, redisClient, logger)
	operator := loginToken(t, engine, "operator")
	reviewer := loginToken(t, engine, "reviewer")

	runPayload := recordPayload("PR-GATE-001", "三点校样门限批次")
	runPayload["deltaELimit"] = 3.0
	status, body := perform(t, engine, http.MethodPost, "/api/runs", operator, "gate-run-create", runPayload)
	if status != http.StatusCreated {
		t.Fatalf("create run status = %d body=%s", status, body)
	}
	run := decodeData[struct {
		ID      uint `json:"id"`
		Version uint `json:"version"`
	}](t, body)
	runPath := "/api/runs/" + uintString(run.ID)
	for _, target := range []string{"printing", "proofing"} {
		status, body = perform(t, engine, http.MethodPost, runPath+"/transition", operator, "gate-run-"+target,
			map[string]any{"status": target, "expectedVersion": run.Version, "reason": "推进批次"})
		if status != http.StatusOK {
			t.Fatalf("run -> %s status = %d body=%s", target, status, body)
		}
		run.Version++
	}

	proofPayload := recordPayload("CP-GATE-001", "三位置校样")
	proofPayload["relatedCode"] = "PR-GATE-001"
	proofPayload["readingOperator"] = 1.5
	proofPayload["readingMiddle"] = 2.0
	status, body = perform(t, engine, http.MethodPost, "/api/proofs", operator, "gate-proof-create", proofPayload)
	if status != http.StatusCreated {
		t.Fatalf("create proof status = %d body=%s", status, body)
	}
	proof := decodeData[struct {
		ID              uint   `json:"id"`
		Version         uint   `json:"version"`
		JudgmentVersion uint   `json:"judgmentVersion"`
		JudgmentResult  string `json:"judgmentResult"`
	}](t, body)
	if proof.JudgmentVersion != 1 || proof.JudgmentResult != "incomplete" {
		t.Fatalf("missing 传动侧 must judge incomplete, got %+v", proof)
	}
	proofPath := "/api/proofs/" + uintString(proof.ID)

	holdReason := func() string {
		_, detailBody := perform(t, engine, http.MethodGet, runPath, operator, "gate-run-read", nil)
		return decodeData[struct {
			HoldReason string `json:"holdReason"`
		}](t, detailBody).HoldReason
	}
	if reason := holdReason(); !strings.Contains(reason, "缺少传动侧读数") {
		t.Fatalf("run should hold for the missing position, got %q", reason)
	}
	release := map[string]any{"status": "released", "expectedVersion": run.Version, "reason": "尝试放行"}
	if status, _ = perform(t, engine, http.MethodPost, runPath+"/transition", reviewer, "gate-release-incomplete", release); status != http.StatusUnprocessableEntity {
		t.Fatalf("release with missing position status = %d, want 422", status)
	}

	if status, body = perform(t, engine, http.MethodPut, proofPath, operator, "gate-proof-fail", proofUpdate("PR-GATE-001", proof.Version, 2.2, 3.4, 2.6)); status != http.StatusOK {
		t.Fatalf("remeasure status = %d body=%s", status, body)
	}
	proof.Version++
	proof.JudgmentVersion++
	if reason := holdReason(); !strings.Contains(reason, "最差位置（中间）ΔE 3.40 超过批次允许 3.00") {
		t.Fatalf("run should hold for the over-limit worst position, got %q", reason)
	}
	if status, _ = perform(t, engine, http.MethodPost, runPath+"/transition", reviewer, "gate-release-fail", release); status != http.StatusUnprocessableEntity {
		t.Fatalf("release with over-limit worst status = %d, want 422", status)
	}

	if status, body = perform(t, engine, http.MethodPut, proofPath, operator, "gate-proof-pass", proofUpdate("PR-GATE-001", proof.Version, 1.5, 2.0, 2.5)); status != http.StatusOK {
		t.Fatalf("remeasure pass status = %d body=%s", status, body)
	}
	proof.Version++
	proof.JudgmentVersion++
	status, body = perform(t, engine, http.MethodPost, proofPath+"/transition", operator, "gate-proof-review",
		map[string]any{"status": "review", "expectedVersion": proof.Version, "reason": "提交复核"})
	if status != http.StatusOK {
		t.Fatalf("proof -> review status = %d body=%s", status, body)
	}
	proof.Version++
	status, body = perform(t, engine, http.MethodPost, proofPath+"/transition", reviewer, "gate-proof-accept",
		map[string]any{"status": "accepted", "expectedVersion": proof.Version, "reason": "复核接收"})
	if status != http.StatusOK {
		t.Fatalf("proof -> accepted status = %d body=%s", status, body)
	}
	proof.Version++
	proof.JudgmentVersion++

	// 复核后改动读数：批次必须停在校样阶段并说明原因。
	if status, body = perform(t, engine, http.MethodPut, proofPath, operator, "gate-proof-tamper", proofUpdate("PR-GATE-001", proof.Version, 1.5, 2.0, 2.6)); status != http.StatusOK {
		t.Fatalf("tamper update status = %d body=%s", status, body)
	}
	proof.Version++
	proof.JudgmentVersion++
	tampered := decodeData[struct {
		JudgmentResult string `json:"judgmentResult"`
	}](t, body)
	if tampered.JudgmentResult != "tampered" {
		t.Fatalf("post-review edit must be judged tampered, got %+v", tampered)
	}
	if reason := holdReason(); !strings.Contains(reason, "复核后读数被改动") {
		t.Fatalf("run should hold for the tampered readings, got %q", reason)
	}
	if status, _ = perform(t, engine, http.MethodPost, runPath+"/transition", reviewer, "gate-release-tampered", release); status != http.StatusUnprocessableEntity {
		t.Fatalf("release with tampered readings status = %d, want 422", status)
	}

	// 复核员重新接收当前读数，生成新的判定版本后放行。
	for _, step := range []struct {
		token, target, requestID string
	}{
		{reviewer, "review", "gate-proof-reopen"},
		{reviewer, "accepted", "gate-proof-reaccept"},
	} {
		status, body = perform(t, engine, http.MethodPost, proofPath+"/transition", step.token, step.requestID,
			map[string]any{"status": step.target, "expectedVersion": proof.Version, "reason": "重新复核当前读数"})
		if status != http.StatusOK {
			t.Fatalf("proof -> %s status = %d body=%s", step.target, status, body)
		}
		proof.Version++
	}
	proof.JudgmentVersion++
	status, body = perform(t, engine, http.MethodPost, runPath+"/transition", reviewer, "gate-release-ok", release)
	if status != http.StatusOK {
		t.Fatalf("release after re-review status = %d body=%s", status, body)
	}
	if reason := holdReason(); reason != "" {
		t.Fatalf("released run must not carry a hold reason, got %q", reason)
	}

	// 旧结论留在判定历史里，现场能看到本批使用的是哪次结果。
	_, body = perform(t, engine, http.MethodGet, proofPath, reviewer, "gate-proof-read", nil)
	detail := decodeData[struct {
		Judgments []struct {
			Version uint   `json:"version"`
			Result  string `json:"result"`
		} `json:"judgments"`
	}](t, body)
	if len(detail.Judgments) != int(proof.JudgmentVersion) || detail.Judgments[0].Version != proof.JudgmentVersion {
		t.Fatalf("judgment chain should keep every version, got %+v", detail.Judgments)
	}
	_, body = perform(t, engine, http.MethodGet, runPath+"/proofs", operator, "gate-run-proofs", nil)
	linked := decodeData[[]struct {
		Code            string `json:"code"`
		JudgmentVersion uint   `json:"judgmentVersion"`
		JudgmentResult  string `json:"judgmentResult"`
	}](t, body)
	if len(linked) != 1 || linked[0].Code != "CP-GATE-001" || linked[0].JudgmentVersion != proof.JudgmentVersion || linked[0].JudgmentResult != "pass" {
		t.Fatalf("batch must expose the judgment version in force, got %+v", linked)
	}
}

func proofUpdate(runCode string, version uint, operator, middle, drive float64) map[string]any {
	payload := recordPayload("ignored", "三位置校样")
	payload["relatedCode"] = runCode
	payload["expectedVersion"] = version
	payload["readingOperator"] = operator
	payload["readingMiddle"] = middle
	payload["readingDrive"] = drive
	return payload
}

func testConfig(dsn string) config.Config {
	return config.Config{
		AppName: "print-color-calibration-release", Environment: "test", Port: "0",
		DatabaseDriver: "sqlite", DatabaseDSN: dsn, JWTSecret: "gb517-router-tests-secret",
		TokenTTL: time.Hour, RequestLimit: 1000, StartupTimeout: time.Second,
		ShutdownTimeout: time.Second, ReadHeaderTimeout: time.Second, ReadTimeout: time.Second,
		WriteTimeout: time.Second, IdleTimeout: time.Second,
	}
}

func loginToken(t *testing.T, engine *gin.Engine, username string) string {
	t.Helper()
	status, body := perform(t, engine, http.MethodPost, "/api/auth/login", "", "login-"+username, map[string]any{"username": username, "password": "Admin123!"})
	if status != http.StatusOK {
		t.Fatalf("login %s status = %d body=%s", username, status, body)
	}
	return decodeData[struct {
		Token string `json:"token"`
	}](t, body).Token
}

func recordPayload(code, name string) map[string]any {
	return map[string]any{
		"code": code, "name": name, "description": "router integration test",
		"facility": "测试印刷区", "owner": "operator", "category": "校准",
		"riskLevel": "medium", "metricValue": 2.1, "metricUnit": "dE",
		"effectiveAt": time.Now().UTC().Format(time.RFC3339), "evidence": "spectrophotometer evidence", "relatedCode": "PR-001",
	}
}

func perform(t *testing.T, engine *gin.Engine, method, path, token, requestID string, payload any) (int, []byte) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, body)
	request.Header.Set("X-Request-ID", requestID)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, request)
	return response.Code, response.Body.Bytes()
}

func decodeData[T any](t *testing.T, body []byte) T {
	t.Helper()
	var envelope apiEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode envelope %s: %v", body, err)
	}
	var value T
	if err := json.Unmarshal(envelope.Data, &value); err != nil {
		t.Fatalf("decode data %s: %v", envelope.Data, err)
	}
	return value
}

func uintString(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
