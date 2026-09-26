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

type ColorProofService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.ColorProof], error)
	Get(context.Context, uint) (model.ColorProof, error)
	Create(context.Context, dto.CreateColorProof, string, string) (model.ColorProof, error)
	Update(context.Context, uint, dto.UpdateColorProof, string, string) (model.ColorProof, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string, string) (model.ColorProof, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
}

type colorProofService struct {
	repository repository.ColorProofRepository
	runs       repository.PrintRunRepository
	security   SecurityService
}

func NewColorProofService(repo repository.ColorProofRepository, runs repository.PrintRunRepository, security SecurityService) ColorProofService {
	return &colorProofService{repository: repo, runs: runs, security: security}
}

func (s *colorProofService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.ColorProof], error) {
	return s.repository.List(ctx, query)
}

func (s *colorProofService) Get(ctx context.Context, id uint) (model.ColorProof, error) {
	return s.repository.Get(ctx, id)
}

func (s *colorProofService) Create(ctx context.Context, input dto.CreateColorProof, actor, requestID string) (model.ColorProof, error) {
	if err := validateColorProofBusinessFields(input.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.ColorProof{}, err
	}
	item := model.ColorProof{
		BaseModel: model.BaseModel{
			Code: strings.ToUpper(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name),
			Status: model.ColorProofInitialStatus, Version: 1, Description: strings.TrimSpace(input.Description),
		},
		Facility: strings.TrimSpace(input.Facility), Owner: strings.TrimSpace(input.Owner),
		Category: strings.TrimSpace(input.Category), RiskLevel: input.RiskLevel,
		MetricUnit:  proofMetricUnit(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode:     strings.ToUpper(strings.TrimSpace(input.RelatedCode)),
		ReadingOperator: input.ReadingOperator, ReadingMiddle: input.ReadingMiddle, ReadingDrive: input.ReadingDrive,
	}
	judgment := s.nextJudgment(ctx, &item, actor, requestID, false)
	if err := s.repository.CreateWithJudgment(ctx, &item, &judgment); err != nil {
		return model.ColorProof{}, fmt.Errorf("create 色彩校样: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "create", "ColorProof", item.ID, "", item.Status, "created 色彩校样")
	refreshRunHoldReason(ctx, s.runs, s.repository, item.RelatedCode)
	return item, nil
}

func (s *colorProofService) Update(ctx context.Context, id uint, input dto.UpdateColorProof, actor, requestID string) (model.ColorProof, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.ColorProof{}, err
	}
	if err := validateColorProofBusinessFields(current.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.ColorProof{}, err
	}
	previousRunCode := current.RelatedCode
	readingsChanged := !sameReading(current.ReadingOperator, input.ReadingOperator) ||
		!sameReading(current.ReadingMiddle, input.ReadingMiddle) ||
		!sameReading(current.ReadingDrive, input.ReadingDrive)
	current.Name = strings.TrimSpace(input.Name)
	current.Description = strings.TrimSpace(input.Description)
	current.Facility = strings.TrimSpace(input.Facility)
	current.Owner = strings.TrimSpace(input.Owner)
	current.Category = strings.TrimSpace(input.Category)
	current.RiskLevel = input.RiskLevel
	current.MetricUnit = proofMetricUnit(input.MetricUnit)
	current.EffectiveAt = input.EffectiveAt.UTC()
	current.Evidence = strings.TrimSpace(input.Evidence)
	current.RelatedCode = strings.ToUpper(strings.TrimSpace(input.RelatedCode))
	current.ReadingOperator = input.ReadingOperator
	current.ReadingMiddle = input.ReadingMiddle
	current.ReadingDrive = input.ReadingDrive
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if readingsChanged || current.RelatedCode != previousRunCode {
		// A reading change after the proof was reviewed means the conclusion
		// no longer covers the current measurements: record it as tampered.
		tampered := readingsChanged && (current.Status == "accepted" || current.Status == "rejected")
		judgment := s.nextJudgment(ctx, &current, actor, requestID, tampered)
		if err := s.repository.UpdateWithJudgment(ctx, id, input.ExpectedVersion, &current, &judgment); err != nil {
			return model.ColorProof{}, fmt.Errorf("update 色彩校样: %w", err)
		}
	} else {
		current.MetricValue = input.MetricValue
		if err := s.repository.Update(ctx, id, input.ExpectedVersion, &current); err != nil {
			return model.ColorProof{}, fmt.Errorf("update 色彩校样: %w", err)
		}
	}
	_ = s.security.Audit(ctx, actor, requestID, "update", "ColorProof", id, current.Status, current.Status, "updated business fields")
	refreshRunHoldReason(ctx, s.runs, s.repository, previousRunCode)
	refreshRunHoldReason(ctx, s.runs, s.repository, current.RelatedCode)
	return s.repository.Get(ctx, id)
}

func (s *colorProofService) Transition(ctx context.Context, id uint, input dto.TransitionRequest, actor, role, requestID string) (model.ColorProof, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.ColorProof{}, err
	}
	target := strings.TrimSpace(input.Status)
	if (target == "accepted" || target == "rejected" || current.Status == "accepted" || current.Status == "rejected") && !canReview(role) {
		return model.ColorProof{}, ErrForbidden
	}
	if !constants.CanTransition(constants.ColorProofTransitions, current.Status, target) {
		return model.ColorProof{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	before := current.Status
	current.Status = target
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if target == "accepted" || target == "rejected" {
		// The review freezes the readings it saw; later edits are detected as
		// 复核后改动 against this snapshot. The review itself appends a fresh
		// judgment so re-review after re-measurement clears a tampered mark.
		current.ReviewedOperator = current.ReadingOperator
		current.ReviewedMiddle = current.ReadingMiddle
		current.ReviewedDrive = current.ReadingDrive
		judgment := s.nextJudgment(ctx, &current, actor, requestID, false)
		if err := s.repository.UpdateWithJudgment(ctx, id, input.ExpectedVersion, &current, &judgment); err != nil {
			return model.ColorProof{}, fmt.Errorf("transition 色彩校样: %w", err)
		}
	} else {
		if err := s.repository.Update(ctx, id, input.ExpectedVersion, &current); err != nil {
			return model.ColorProof{}, fmt.Errorf("transition 色彩校样: %w", err)
		}
	}
	if err := s.security.Audit(ctx, actor, requestID, "transition", "ColorProof", id, before, target, input.Reason); err != nil {
		return model.ColorProof{}, fmt.Errorf("persist transition audit: %w", err)
	}
	refreshRunHoldReason(ctx, s.runs, s.repository, current.RelatedCode)
	return s.repository.Get(ctx, id)
}

func (s *colorProofService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, id); err != nil {
		return err
	}
	return s.security.Audit(ctx, actor, requestID, "delete", "ColorProof", id, current.Status, "deleted", "soft deleted 色彩校样")
}

func (s *colorProofService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

// nextJudgment evaluates the proof's three-position readings against the
// linked batch tolerance, stamps the denormalized conclusion onto the proof
// and returns the 判定版本 to append.
func (s *colorProofService) nextJudgment(ctx context.Context, item *model.ColorProof, actor, requestID string, tampered bool) model.ProofJudgment {
	tolerance, hasLimit := s.toleranceFor(ctx, item.RelatedCode)
	evaluation := evaluateProofJudgment(proofReadings(item), tolerance, hasLimit, tampered)
	return applyEvaluation(item, evaluation, tolerance, actor, requestID)
}

func (s *colorProofService) toleranceFor(ctx context.Context, runCode string) (float64, bool) {
	if strings.TrimSpace(runCode) == "" {
		return 0, false
	}
	run, err := s.runs.GetByCode(ctx, runCode)
	if err != nil || run.DeltaELimit <= 0 {
		return 0, false
	}
	return run.DeltaELimit, true
}

func sameReading(current, next *float64) bool {
	if current == nil || next == nil {
		return current == nil && next == nil
	}
	return *current == *next
}

func proofMetricUnit(unit string) string {
	if strings.TrimSpace(unit) == "" {
		return "ΔE"
	}
	return strings.TrimSpace(unit)
}

func validateColorProofBusinessFields(code, name, facility, owner string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" {
		return ErrInvalidInput
	}
	return nil
}
