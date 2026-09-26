package service_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blueship581/print-color-calibration-release/backend/internal/config"
	"github.com/blueship581/print-color-calibration-release/backend/internal/constants"
	"github.com/blueship581/print-color-calibration-release/backend/internal/dto"
	"github.com/blueship581/print-color-calibration-release/backend/internal/model"
	"github.com/blueship581/print-color-calibration-release/backend/internal/repository"
	"github.com/blueship581/print-color-calibration-release/backend/internal/service"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type gateFixture struct {
	db       *gorm.DB
	runs     service.PrintRunService
	proofs   service.ColorProofService
	security service.SecurityService
}

func newGateFixture(t *testing.T) gateFixture {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "gate.db")), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.User{}, &model.AuditLog{}, &model.PressUnit{},
		&model.PrintRun{}, &model.PrintRunRevision{},
		&model.ColorProof{}, &model.ColorProofJudgment{},
		&model.ReleaseDecision{}, &model.ReleaseDecisionRevision{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	cfg := config.Config{AppName: "gate-test", Environment: "test", JWTSecret: "secret", TokenTTL: time.Hour}
	security := service.NewSecurityService(repository.NewSecurityRepository(db), cfg)
	runRepo := repository.NewPrintRunRepository(db)
	proofRepo := repository.NewColorProofRepository(db)
	runs := service.NewPrintRunService(runRepo, security)
	proofs := service.NewColorProofService(proofRepo, security, runs)
	return gateFixture{db: db, runs: runs, proofs: proofs, security: security}
}

func createProofingRun(t *testing.T, f gateFixture, code string, tolerance float64) model.PrintRun {
	t.Helper()
	ctx := context.Background()
	run, err := f.runs.Create(ctx, dto.CreatePrintRun{
		Code: code, Name: "校样门控测试批次", Facility: "车间", Owner: "operator",
		Category: "常规", RiskLevel: "medium", MetricUnit: "ΔE",
		EffectiveAt: time.Now().UTC(), Evidence: "setup", ColorTolerance: tolerance,
	}, "operator", "run-create")
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	run, err = f.runs.Transition(ctx, run.ID, dto.TransitionRequest{
		Status: "printing", ExpectedVersion: run.Version, Reason: "plates and ink verified",
	}, "operator", "operator", "run-printing")
	if err != nil {
		t.Fatalf("to printing: %v", err)
	}
	run, err = f.runs.Transition(ctx, run.ID, dto.TransitionRequest{
		Status: "proofing", ExpectedVersion: run.Version, Reason: "proof strip ready",
	}, "operator", "operator", "run-proofing")
	if err != nil {
		t.Fatalf("to proofing: %v", err)
	}
	return run
}

func ptr(v float64) *float64 { return &v }

func createProof(t *testing.T, f gateFixture, code, runCode string, op, center, drive *float64) model.ColorProof {
	t.Helper()
	proof, err := f.proofs.Create(context.Background(), dto.CreateColorProof{
		Code: code, Name: "三位置校样", Facility: "车间", Owner: "operator",
		Category: "校准", RiskLevel: "medium", MetricUnit: "ΔE",
		EffectiveAt: time.Now().UTC(), Evidence: "spectrophotometer",
		RelatedCode: runCode, OperationSide: op, Center: center, DriveSide: drive,
	}, "operator", "proof-create")
	if err != nil {
		t.Fatalf("create proof: %v", err)
	}
	return proof
}

func submitAndAccept(t *testing.T, f gateFixture, proof model.ColorProof) (model.ColorProof, error) {
	t.Helper()
	ctx := context.Background()
	review, err := f.proofs.Transition(ctx, proof.ID, dto.TransitionRequest{
		Status: "review", ExpectedVersion: proof.Version, Reason: "three positions captured",
	}, "operator", "operator", "proof-review")
	if err != nil {
		return model.ColorProof{}, err
	}
	return f.proofs.Transition(ctx, review.ID, dto.TransitionRequest{
		Status: "accepted", ExpectedVersion: review.Version, Reason: "worst position within tolerance",
	}, "reviewer", "reviewer", "proof-accept")
}

func reloadProof(t *testing.T, f gateFixture, id uint) model.ColorProof {
	t.Helper()
	proof, err := f.proofs.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("reload proof: %v", err)
	}
	return proof
}

func reloadRun(t *testing.T, f gateFixture, code string) model.PrintRun {
	t.Helper()
	run, err := f.runs.Get(context.Background(), mustRunID(t, f, code))
	if err != nil {
		t.Fatalf("reload run: %v", err)
	}
	return run
}

func mustRunID(t *testing.T, f gateFixture, code string) uint {
	t.Helper()
	var run model.PrintRun
	if err := f.db.Where("code = ?", code).First(&run).Error; err != nil {
		t.Fatalf("find run %s: %v", code, err)
	}
	return run.ID
}

// Missing readings block review submission and hold the batch at proofing.
func TestProofMissingPositionHoldsBatch(t *testing.T) {
	f := newGateFixture(t)
	ctx := context.Background()
	createProofingRun(t, f, "PR-G1", 3.0)
	proof := createProof(t, f, "CP-G1", "PR-G1", ptr(1.2), ptr(1.5), nil)

	_, err := f.proofs.Transition(ctx, proof.ID, dto.TransitionRequest{
		Status: "review", ExpectedVersion: proof.Version, Reason: "attempt submit missing drive side",
	}, "operator", "operator", "proof-review-missing")
	if err == nil {
		t.Fatal("expected review submit with missing reading to fail")
	}
	proof = reloadProof(t, f, proof.ID)
	if proof.Status != "captured" {
		t.Fatalf("proof status = %s, want captured", proof.Status)
	}
	active := activeJudgmentOf(t, proof)
	if active.HoldReason == "" || active.Conclusion != model.ProofJudgmentPending {
		t.Fatalf("active judgment should carry the missing-reading hold reason: %+v", active)
	}
	run := reloadRun(t, f, "PR-G1")
	if run.Status != "hold" || run.HoldReason == "" {
		t.Fatalf("run should be held with reason, got status=%s reason=%q", run.Status, run.HoldReason)
	}
}

// Worst position over tolerance blocks acceptance and holds the batch.
func TestProofWorstPositionOverTolerance(t *testing.T) {
	f := newGateFixture(t)
	ctx := context.Background()
	createProofingRun(t, f, "PR-G2", 3.0)
	// Centre is within limit but drive side exceeds it: averaging would hide it.
	proof := createProof(t, f, "CP-G2", "PR-G2", ptr(1.1), ptr(1.4), ptr(3.8))
	if proof.WorstPosition != model.ProofPositionDrive || proof.MetricValue != 3.8 {
		t.Fatalf("worst = (%s, %.2f), want (drive, 3.80)", proof.WorstPosition, proof.MetricValue)
	}
	review, err := f.proofs.Transition(ctx, proof.ID, dto.TransitionRequest{
		Status: "review", ExpectedVersion: proof.Version, Reason: "three positions captured",
	}, "operator", "operator", "proof-review")
	if err != nil {
		t.Fatalf("submit review: %v", err)
	}
	if _, err := f.proofs.Transition(ctx, review.ID, dto.TransitionRequest{
		Status: "accepted", ExpectedVersion: review.Version, Reason: "try to accept over-limit proof",
	}, "reviewer", "reviewer", "proof-accept-over"); err == nil {
		t.Fatal("expected acceptance over worst-position tolerance to fail")
	}
	run := reloadRun(t, f, "PR-G2")
	if run.Status != "hold" {
		t.Fatalf("run status = %s, want hold", run.Status)
	}
	if !strings.Contains(run.HoldReason, "传动侧") || !strings.Contains(run.HoldReason, "3.80") {
		t.Fatalf("hold reason should name drive side and value: %q", run.HoldReason)
	}
	proof = reloadProof(t, f, proof.ID)
	if proof.Status != "review" {
		t.Fatalf("proof status = %s, want review", proof.Status)
	}
}

// Acceptance judges the worst position and releases the proof gate.
func TestProofAcceptedOnWorstPosition(t *testing.T) {
	f := newGateFixture(t)
	createProofingRun(t, f, "PR-G3", 3.0)
	proof := createProof(t, f, "CP-G3", "PR-G3", ptr(2.9), ptr(1.2), ptr(2.4))
	accepted, err := submitAndAccept(t, f, proof)
	if err != nil {
		t.Fatalf("accept within tolerance: %v", err)
	}
	if accepted.Status != "accepted" {
		t.Fatalf("status = %s", accepted.Status)
	}
	active := activeJudgmentOf(t, accepted)
	if active.Conclusion != model.ProofJudgmentAccepted || active.WorstPosition != model.ProofPositionOperation || active.WorstValue != 2.9 {
		t.Fatalf("active judgment mismatch: %+v", active)
	}
	run := reloadRun(t, f, "PR-G3")
	if run.Status != "proofing" || run.HoldReason != "" {
		t.Fatalf("run should be back at proofing with cleared reason: %s %q", run.Status, run.HoldReason)
	}
}

// Changing readings after review produces a new judgment version, keeps the
// old conclusion in history, and holds the batch pending re-review.
func TestProofTamperingAfterReviewCreatesVersion(t *testing.T) {
	f := newGateFixture(t)
	ctx := context.Background()
	createProofingRun(t, f, "PR-G4", 3.0)
	proof := createProof(t, f, "CP-G4", "PR-G4", ptr(1.2), ptr(1.5), ptr(1.8))
	accepted, err := submitAndAccept(t, f, proof)
	if err != nil {
		t.Fatalf("accept: %v", err)
	}

	updated, err := f.proofs.Update(ctx, accepted.ID, dto.UpdateColorProof{
		ExpectedVersion: accepted.Version, Name: accepted.Name, Facility: accepted.Facility,
		Owner: accepted.Owner, Category: accepted.Category, RiskLevel: accepted.RiskLevel,
		MetricUnit: accepted.MetricUnit, EffectiveAt: accepted.EffectiveAt, Evidence: accepted.Evidence,
		RelatedCode: "PR-G4", OperationSide: ptr(1.2), Center: ptr(1.5), DriveSide: ptr(4.2),
	}, "operator", "proof-reread")
	if err != nil {
		t.Fatalf("re-measure: %v", err)
	}
	if updated.Status != "captured" {
		t.Fatalf("tampered proof status = %s, want captured", updated.Status)
	}
	if len(updated.Judgments) < 2 {
		t.Fatalf("expected >= 2 judgments, got %d", len(updated.Judgments))
	}
	// Judgments are preloaded newest first.
	if !updated.Judgments[0].Active || updated.Judgments[0].JudgmentNo != 2 || updated.Judgments[0].Conclusion != model.ProofJudgmentPending {
		t.Fatalf("new active judgment mismatch: %+v", updated.Judgments[0])
	}
	if updated.Judgments[1].Conclusion != model.ProofJudgmentAccepted || updated.Judgments[1].Active {
		t.Fatalf("old accepted judgment must remain in history inactive: %+v", updated.Judgments[1])
	}
	run := reloadRun(t, f, "PR-G4")
	if run.Status != "hold" || !strings.Contains(run.HoldReason, "读数被改动") {
		t.Fatalf("run held for tampering, got %s %q", run.Status, run.HoldReason)
	}

	// Re-measure within limit and review again: new judgment v3 accepted, gate clears.
	accepted2, err := f.proofs.Update(ctx, updated.ID, dto.UpdateColorProof{
		ExpectedVersion: updated.Version, Name: updated.Name, Facility: updated.Facility,
		Owner: updated.Owner, Category: updated.Category, RiskLevel: updated.RiskLevel,
		MetricUnit: updated.MetricUnit, EffectiveAt: updated.EffectiveAt, Evidence: updated.Evidence,
		RelatedCode: "PR-G4", OperationSide: ptr(1.1), Center: ptr(1.3), DriveSide: ptr(1.7),
	}, "operator", "proof-reread-ok")
	if err != nil {
		t.Fatalf("second re-measure: %v", err)
	}
	final, err := submitAndAccept(t, f, accepted2)
	if err != nil {
		t.Fatalf("re-review after re-measure: %v", err)
	}
	if final.Judgments[0].JudgmentNo != 3 || final.Judgments[0].Conclusion != model.ProofJudgmentAccepted {
		t.Fatalf("expected active v3 accepted: %+v", final.Judgments[0])
	}
	if len(final.Judgments) != 3 {
		t.Fatalf("full history should keep three rounds, got %d", len(final.Judgments))
	}
	run = reloadRun(t, f, "PR-G4")
	if run.Status != "proofing" || run.HoldReason != "" {
		t.Fatalf("gate should clear after re-accept: %s %q", run.Status, run.HoldReason)
	}
}

// Non-measurement edits must not create a judgment version.
func TestProofNonReadingEditKeepsJudgment(t *testing.T) {
	f := newGateFixture(t)
	ctx := context.Background()
	proof := createProof(t, f, "CP-G5", "", ptr(1.0), ptr(1.0), ptr(1.0))
	updated, err := f.proofs.Update(ctx, proof.ID, dto.UpdateColorProof{
		ExpectedVersion: proof.Version, Name: "改名后的校样", Facility: proof.Facility,
		Owner: proof.Owner, Category: proof.Category, RiskLevel: proof.RiskLevel,
		MetricUnit: proof.MetricUnit, EffectiveAt: proof.EffectiveAt, Evidence: proof.Evidence,
		RelatedCode: "", OperationSide: ptr(1.0), Center: ptr(1.0), DriveSide: ptr(1.0),
	}, "operator", "proof-rename")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	reloaded := reloadProof(t, f, updated.ID)
	if len(reloaded.Judgments) != 1 || reloaded.Judgments[0].JudgmentNo != 1 {
		t.Fatalf("non-reading edit must not add a judgment: %+v", reloaded.Judgments)
	}
}

// Rejected proofs park the batch at proofing with the rejection reason.
func TestProofRejectionHoldsBatch(t *testing.T) {
	f := newGateFixture(t)
	ctx := context.Background()
	createProofingRun(t, f, "PR-G6", 3.0)
	proof := createProof(t, f, "CP-G6", "PR-G6", ptr(1.0), ptr(1.1), ptr(1.2))
	review, err := f.proofs.Transition(ctx, proof.ID, dto.TransitionRequest{
		Status: "review", ExpectedVersion: proof.Version, Reason: "three positions captured",
	}, "operator", "operator", "proof-review")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	rejected, err := f.proofs.Transition(ctx, review.ID, dto.TransitionRequest{
		Status: "rejected", ExpectedVersion: review.Version, Reason: "visible banding on strip",
	}, "reviewer", "reviewer", "proof-reject")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if rejected.Status != "rejected" || activeJudgmentOf(t, rejected).Conclusion != model.ProofJudgmentRejected {
		t.Fatalf("rejection mismatch: %+v", rejected)
	}
	run := reloadRun(t, f, "PR-G6")
	if run.Status != "hold" || !strings.Contains(run.HoldReason, "复核未通过") {
		t.Fatalf("run held after rejection: %s %q", run.Status, run.HoldReason)
	}
}

// Re-measuring after a rejection is the normal correction path: it creates a
// new judgment but does not flag a tamper hold beyond the rejection block.
func TestProofRemeasureAfterRejection(t *testing.T) {
	f := newGateFixture(t)
	ctx := context.Background()
	createProofingRun(t, f, "PR-G7", 3.0)
	proof := createProof(t, f, "CP-G7", "PR-G7", ptr(1.0), ptr(1.1), ptr(1.2))
	review, err := f.proofs.Transition(ctx, proof.ID, dto.TransitionRequest{
		Status: "review", ExpectedVersion: proof.Version, Reason: "three positions captured",
	}, "operator", "operator", "proof-review")
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	rejected, err := f.proofs.Transition(ctx, review.ID, dto.TransitionRequest{
		Status: "rejected", ExpectedVersion: review.Version, Reason: "banding visible",
	}, "reviewer", "reviewer", "proof-reject")
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	updated, err := f.proofs.Update(ctx, rejected.ID, dto.UpdateColorProof{
		ExpectedVersion: rejected.Version, Name: rejected.Name, Facility: rejected.Facility,
		Owner: rejected.Owner, Category: rejected.Category, RiskLevel: rejected.RiskLevel,
		MetricUnit: rejected.MetricUnit, EffectiveAt: rejected.EffectiveAt, Evidence: rejected.Evidence,
		RelatedCode: "PR-G7", OperationSide: ptr(0.9), Center: ptr(1.0), DriveSide: ptr(1.1),
	}, "operator", "proof-reread-after-reject")
	if err != nil {
		t.Fatalf("re-measure after rejection: %v", err)
	}
	active := activeJudgmentOf(t, updated)
	if active.HoldReason != "" {
		t.Fatalf("post-rejection re-measure must not flag tamper reason: %q", active.HoldReason)
	}
	if updated.Status != "captured" {
		t.Fatalf("status = %s, want captured", updated.Status)
	}
}

func TestStateMachineUnchanged(t *testing.T) {
	if !constants.CanTransition(constants.PrintRunTransitions, "hold", "proofing") {
		t.Fatal("hold -> proofing must remain valid for re-measurement")
	}
}

func activeJudgmentOf(t *testing.T, p model.ColorProof) model.ColorProofJudgment {
	t.Helper()
	for _, judgment := range p.Judgments {
		if judgment.Active {
			return judgment
		}
	}
	t.Fatalf("proof %s has no active judgment", p.Code)
	return model.ColorProofJudgment{}
}
