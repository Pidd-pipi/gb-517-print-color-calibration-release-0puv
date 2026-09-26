package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/print-color-calibration-release/backend/internal/constants"
	"github.com/blueship581/print-color-calibration-release/backend/internal/dto"
	"github.com/blueship581/print-color-calibration-release/backend/internal/model"
	"github.com/blueship581/print-color-calibration-release/backend/internal/repository"
)

type PrintRunService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.PrintRun], error)
	Get(context.Context, uint) (model.PrintRun, error)
	Create(context.Context, dto.CreatePrintRun, string, string) (model.PrintRun, error)
	Update(context.Context, uint, dto.UpdatePrintRun, string, string) (model.PrintRun, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string, string) (model.PrintRun, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
	RunProofGate
}

var _ RunProofGate = (*printRunService)(nil)

type printRunService struct {
	repository repository.PrintRunRepository
	security   SecurityService
}

func NewPrintRunService(repo repository.PrintRunRepository, security SecurityService) PrintRunService {
	return &printRunService{repository: repo, security: security}
}

func (s *printRunService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.PrintRun], error) {
	return s.repository.List(ctx, query)
}

func (s *printRunService) Get(ctx context.Context, id uint) (model.PrintRun, error) {
	return s.repository.Get(ctx, id)
}

func (s *printRunService) Create(ctx context.Context, input dto.CreatePrintRun, actor, requestID string) (model.PrintRun, error) {
	if err := validatePrintRunBusinessFields(input.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.PrintRun{}, err
	}
	item := model.PrintRun{
		BaseModel: model.BaseModel{
			Code: strings.ToUpper(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name),
			Status: model.PrintRunInitialStatus, Version: 1, Description: strings.TrimSpace(input.Description),
		},
		Facility: strings.TrimSpace(input.Facility), Owner: strings.TrimSpace(input.Owner),
		Category: strings.TrimSpace(input.Category), RiskLevel: input.RiskLevel,
		MetricValue: input.MetricValue, MetricUnit: strings.TrimSpace(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode:    strings.ToUpper(strings.TrimSpace(input.RelatedCode)),
		ColorTolerance: normalizeTolerance(input.ColorTolerance),
	}
	if err := s.repository.CreateVersioned(ctx, &item, actor, requestID, "created colour configuration"); err != nil {
		return model.PrintRun{}, fmt.Errorf("create 印刷批次: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "create", "PrintRun", item.ID, "", item.Status, "created 印刷批次")
	return item, nil
}

func (s *printRunService) Update(ctx context.Context, id uint, input dto.UpdatePrintRun, actor, requestID string) (model.PrintRun, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.PrintRun{}, err
	}
	if err := validatePrintRunBusinessFields(current.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.PrintRun{}, err
	}
	current.Name = strings.TrimSpace(input.Name)
	current.Description = strings.TrimSpace(input.Description)
	current.Facility = strings.TrimSpace(input.Facility)
	current.Owner = strings.TrimSpace(input.Owner)
	current.Category = strings.TrimSpace(input.Category)
	current.RiskLevel = input.RiskLevel
	current.MetricValue = input.MetricValue
	current.MetricUnit = strings.TrimSpace(input.MetricUnit)
	current.EffectiveAt = input.EffectiveAt.UTC()
	current.Evidence = strings.TrimSpace(input.Evidence)
	current.RelatedCode = strings.ToUpper(strings.TrimSpace(input.RelatedCode))
	current.ColorTolerance = normalizeTolerance(input.ColorTolerance)
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.UpdateVersioned(ctx, id, input.ExpectedVersion, &current, actor, requestID, "updated colour configuration"); err != nil {
		return model.PrintRun{}, fmt.Errorf("update 印刷批次: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "update", "PrintRun", id, current.Status, current.Status, "updated business fields")
	return s.repository.Get(ctx, id)
}

func (s *printRunService) Transition(ctx context.Context, id uint, input dto.TransitionRequest, actor, role, requestID string) (model.PrintRun, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.PrintRun{}, err
	}
	target := strings.TrimSpace(input.Status)
	if (target == string(constants.RunStateReleased) || current.Status == string(constants.RunStateReleased)) && !canReview(role) {
		return model.PrintRun{}, ErrForbidden
	}
	if !constants.CanTransition(constants.PrintRunTransitions, current.Status, target) {
		return model.PrintRun{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	before := current.Status
	current.Status = target
	// Leaving the proof gate clears whatever blocking reason the proof review
	// recorded, so stale reasons never linger on released/re-printed batches.
	if target != string(constants.RunStateHold) {
		current.HoldReason = ""
	}
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.UpdateVersioned(ctx, id, input.ExpectedVersion, &current, actor, requestID, input.Reason); err != nil {
		return model.PrintRun{}, fmt.Errorf("transition 印刷批次: %w", err)
	}
	if err := s.security.Audit(ctx, actor, requestID, "transition", "PrintRun", id, before, target, input.Reason); err != nil {
		return model.PrintRun{}, fmt.Errorf("persist transition audit: %w", err)
	}
	return s.repository.Get(ctx, id)
}

func canReview(role string) bool { return role == model.RoleReviewer || role == model.RoleAdmin }

func (s *printRunService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if current.Status != model.PrintRunInitialStatus {
		return ErrLocked
	}
	if err := s.repository.Delete(ctx, id); err != nil {
		return err
	}
	return s.security.Audit(ctx, actor, requestID, "delete", "PrintRun", id, current.Status, "deleted", "soft deleted 印刷批次")
}

func (s *printRunService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

// HoldAtProofing implements RunProofGate: the proof service calls it when a
// proof cannot be released (missing position, worst position over tolerance,
// or readings changed after review).
func (s *printRunService) HoldAtProofing(ctx context.Context, code, holdReason, actor, requestID, reason string) (uint, bool, error) {
	if strings.TrimSpace(code) == "" {
		return 0, false, nil
	}
	runID, parked, err := s.repository.HoldAtProofing(ctx, code, strings.TrimSpace(holdReason), actor, requestID, reason)
	if err != nil {
		return 0, false, fmt.Errorf("hold run at proofing: %w", err)
	}
	if parked {
		_ = s.security.Audit(ctx, actor, requestID, "transition", "PrintRun", runID, "proofing", "hold", reason)
	}
	return runID, parked, nil
}

// ResumeProofing implements RunProofGate: after a re-measurement passes review
// the batch returns to proofing and the blocking reason is cleared.
func (s *printRunService) ResumeProofing(ctx context.Context, code, actor, requestID, reason string) error {
	if strings.TrimSpace(code) == "" {
		return nil
	}
	runID, err := s.repository.ResumeProofing(ctx, code, actor, requestID, reason)
	if err != nil {
		return fmt.Errorf("resume run proofing: %w", err)
	}
	if runID != 0 {
		_ = s.security.Audit(ctx, actor, requestID, "transition", "PrintRun", runID, "hold", "proofing", reason)
	}
	return nil
}

// EffectiveTolerance implements RunProofGate by reading the batch limit.
func (s *printRunService) EffectiveTolerance(ctx context.Context, code string) (float64, error) {
	run, err := s.repository.FindByCode(ctx, code)
	if err != nil {
		return DefaultColorTolerance, err
	}
	return normalizeTolerance(run.ColorTolerance), nil
}

func validatePrintRunBusinessFields(code, name, facility, owner string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" {
		return ErrInvalidInput
	}
	return nil
}

// DefaultColorTolerance is the batch ΔE limit used when no per-batch tolerance
// is configured. Judgments compare the worst sheet position against it.
const DefaultColorTolerance = 3.0

// normalizeTolerance falls back to the default for missing/non-positive limits.
func normalizeTolerance(value float64) float64 {
	if value <= 0 {
		return DefaultColorTolerance
	}
	return value
}

// RunProofGate is the subset of PrintRunService used by the proof gate. It
// exists as a narrow interface so the proof service can park/resume batches
// without depending on the concrete print-run service.
type RunProofGate interface {
	// HoldAtProofing parks the batch (proofing/hold only) with the reason the
	// proof review produced. It returns the run ID and true when a run is gated.
	HoldAtProofing(ctx context.Context, code, holdReason, actor, requestID, reason string) (uint, bool, error)
	// ResumeProofing clears the proof-gate hold after re-measurement passes.
	ResumeProofing(ctx context.Context, code, actor, requestID, reason string) error
	// EffectiveTolerance resolves the batch ΔE limit (default when unset).
	EffectiveTolerance(ctx context.Context, code string) (float64, error)
}
