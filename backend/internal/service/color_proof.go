package service

import (
	"context"
	"fmt"
	"math"
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
	security   SecurityService
	runGate    RunProofGate
}

func NewColorProofService(repo repository.ColorProofRepository, security SecurityService, runGate RunProofGate) ColorProofService {
	return &colorProofService{repository: repo, security: security, runGate: runGate}
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
		MetricUnit:  normalizeProofUnit(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode: strings.ToUpper(strings.TrimSpace(input.RelatedCode)),
	}
	applyReadings(&item, readingsFromDTO(input.OperationSide, input.Center, input.DriveSide))
	tolerance := s.toleranceFor(ctx, item.RelatedCode)
	judgment := newPendingJudgment(&item, tolerance, actor, requestID, "initial measurement captured")
	if err := s.repository.Create(ctx, &item, judgment); err != nil {
		return model.ColorProof{}, fmt.Errorf("create 色彩校样: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "create", "ColorProof", item.ID, "", item.Status,
		fmt.Sprintf("created 色彩校样 judgment v1 (%s)", s.readingSummary(item)))
	return s.repository.Get(ctx, item.ID)
}

func (s *colorProofService) Update(ctx context.Context, id uint, input dto.UpdateColorProof, actor, requestID string) (model.ColorProof, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.ColorProof{}, err
	}
	if err := validateColorProofBusinessFields(current.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.ColorProof{}, err
	}
	current.Name = strings.TrimSpace(input.Name)
	current.Description = strings.TrimSpace(input.Description)
	current.Facility = strings.TrimSpace(input.Facility)
	current.Owner = strings.TrimSpace(input.Owner)
	current.Category = strings.TrimSpace(input.Category)
	current.RiskLevel = input.RiskLevel
	current.MetricUnit = normalizeProofUnit(input.MetricUnit)
	current.EffectiveAt = input.EffectiveAt.UTC()
	current.Evidence = strings.TrimSpace(input.Evidence)
	current.RelatedCode = strings.ToUpper(strings.TrimSpace(input.RelatedCode))

	previousReadings := readingsFromProof(&current)
	nextReadings := readingsFromDTO(input.OperationSide, input.Center, input.DriveSide)
	readingsChanged := readingsChanged(previousReadings, nextReadings)
	beforeStatus := current.Status
	applyReadings(&current, nextReadings)
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()

	if !readingsChanged {
		// Non-measurement edits do not produce a new judgment version.
		if err := s.repository.Update(ctx, id, input.ExpectedVersion, &current); err != nil {
			return model.ColorProof{}, fmt.Errorf("update 色彩校样: %w", err)
		}
		_ = s.security.Audit(ctx, actor, requestID, "update", "ColorProof", id, current.Status, current.Status, "updated business fields")
		return s.repository.Get(ctx, id)
	}

	// Tampering means readings changed after an accepted judgment. Re-measuring
	// after rejection is the normal correction path. A fresh measurement round
	// always invalidates the in-flight conclusion: the proof goes back to
	// captured for a new submit and independent review.
	tampered := currentActiveConclusion(current) == model.ProofJudgmentAccepted
	current.Status = model.ColorProofInitialStatus
	tolerance := s.toleranceFor(ctx, current.RelatedCode)
	judgmentReason := "re-measurement after review; previous judgment superseded"
	holdReason := ""
	if !tampered {
		judgmentReason = "readings re-measured before review"
	}
	judgment := newPendingJudgment(&current, tolerance, actor, requestID, judgmentReason)
	if tampered {
		holdReason = proofReasonTampered
		judgment.HoldReason = holdReason
	}
	if err := s.repository.UpdateWithJudgment(ctx, id, input.ExpectedVersion, &current, judgment); err != nil {
		return model.ColorProof{}, fmt.Errorf("update 色彩校样: %w", err)
	}
	auditDetail := fmt.Sprintf("re-measured 色彩校样 judgment v%d (%s)", judgment.JudgmentNo, s.readingSummary(current))
	if tampered {
		auditDetail += "; readings changed after review"
	}
	_ = s.security.Audit(ctx, actor, requestID, "update", "ColorProof", id, beforeStatus, current.Status, auditDetail)
	if tampered {
		// The batch stops at proofing until the new measurement is re-reviewed.
		if _, parked, gateErr := s.runGate.HoldAtProofing(ctx, current.RelatedCode, holdReason, actor, requestID,
			fmt.Sprintf("proof %s %s", current.Code, proofReasonTampered)); gateErr != nil {
			return model.ColorProof{}, gateErr
		} else if parked {
			auditDetail += "; run held at proofing"
		}
	}
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

	tolerance := s.toleranceFor(ctx, current.RelatedCode)
	readings := readingsFromProof(&current)

	switch target {
	case "review":
		// Operator submission: all three positions must be recorded.
		if current.Status == "captured" && !readings.complete() {
			_ = s.repository.SetActiveJudgment(ctx, id, model.ProofJudgmentPending, proofReasonMissingReading)
			s.parkRunForProof(ctx, current, proofReasonMissingReading, actor, requestID)
			return model.ColorProof{}, fmt.Errorf("%w: %s", ErrInvalidInput, proofReasonMissingReading)
		}
	case "accepted":
		if !readings.complete() {
			_ = s.repository.SetActiveJudgment(ctx, id, model.ProofJudgmentPending, proofReasonMissingReading)
			s.parkRunForProof(ctx, current, proofReasonMissingReading, actor, requestID)
			return model.ColorProof{}, fmt.Errorf("%w: %s", ErrInvalidInput, proofReasonMissingReading)
		}
		worst, position := readings.worst()
		if worst > tolerance {
			reason := proofOverToleranceReason(position, worst, tolerance)
			_ = s.repository.SetActiveJudgment(ctx, id, model.ProofJudgmentPending, reason)
			s.parkRunForProof(ctx, current, reason, actor, requestID)
			return model.ColorProof{}, fmt.Errorf("%w: %s", ErrInvalidInput, reason)
		}
	case "rejected":
		// Rejection itself parks the batch; reason recorded below.
	}

	before := current.Status
	conclusion := model.ProofJudgmentPending
	holdReason := ""
	switch target {
	case "accepted":
		conclusion = model.ProofJudgmentAccepted
	case "rejected":
		conclusion = model.ProofJudgmentRejected
		holdReason = fmt.Sprintf("校样复核未通过：%s", strings.TrimSpace(input.Reason))
	}
	current.Status = target
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.ReviewUpdate(ctx, id, input.ExpectedVersion, &current, conclusion, holdReason); err != nil {
		return model.ColorProof{}, fmt.Errorf("transition 色彩校样: %w", err)
	}
	if err := s.security.Audit(ctx, actor, requestID, "transition", "ColorProof", id, before, target,
		fmt.Sprintf("%s; %s", input.Reason, s.readingSummary(current))); err != nil {
		return model.ColorProof{}, fmt.Errorf("persist transition audit: %w", err)
	}

	switch target {
	case "accepted":
		// Passing judgment clears the proof gate; the batch returns to proofing
		// and the separate release transition remains a reviewer decision.
		if s.runGate != nil && strings.TrimSpace(current.RelatedCode) != "" {
			if err := s.runGate.ResumeProofing(ctx, current.RelatedCode, actor, requestID,
				fmt.Sprintf("proof %s accepted on worst position %.2f <= %.2f", current.Code, readingsWorst(readings), tolerance)); err != nil {
				return model.ColorProof{}, err
			}
		}
	case "rejected":
		s.parkRunForProof(ctx, current, holdReason, actor, requestID)
	}
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

// parkRunForProof holds the linked batch at proofing with the blocking reason.
func (s *colorProofService) parkRunForProof(ctx context.Context, proof model.ColorProof, reason, actor, requestID string) {
	if s.runGate == nil || strings.TrimSpace(proof.RelatedCode) == "" {
		return
	}
	_, _, _ = s.runGate.HoldAtProofing(ctx, proof.RelatedCode, reason, actor, requestID,
		fmt.Sprintf("proof %s blocked: %s", proof.Code, reason))
}

func (s *colorProofService) toleranceFor(ctx context.Context, code string) float64 {
	if s.runGate == nil || strings.TrimSpace(code) == "" {
		return DefaultColorTolerance
	}
	tolerance, err := s.runGate.EffectiveTolerance(ctx, code)
	if err != nil || tolerance <= 0 {
		return DefaultColorTolerance
	}
	return tolerance
}

func (s *colorProofService) readingSummary(p model.ColorProof) string {
	readings := readingsFromProof(&p)
	worst, position := readings.worst()
	return fmt.Sprintf("操作侧=%s 中间=%s 传动侧=%s 最差=%s(%.2f)",
		formatReading(readings.operation), formatReading(readings.center), formatReading(readings.drive),
		proofPositionLabel(position), worst)
}

func formatReading(value *float64) string {
	if value == nil {
		return "缺录"
	}
	return fmt.Sprintf("%.2f", *value)
}

func readingsWorst(r proofReadingSet) float64 {
	worst, _ := r.worst()
	return worst
}

// currentActiveConclusion returns the conclusion of the latest judgment round
// ("" when no judgment has been recorded).
func currentActiveConclusion(p model.ColorProof) string {
	if judgment := activeJudgment(p); judgment != nil {
		return judgment.Conclusion
	}
	return ""
}

func normalizeProofUnit(unit string) string {
	unit = strings.TrimSpace(unit)
	if unit == "" {
		return "ΔE"
	}
	return unit
}

func validateColorProofBusinessFields(code, name, facility, owner string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" {
		return ErrInvalidInput
	}
	return nil
}

// Proof block reasons surfaced on the batch and in the UI. They are stable
// Chinese strings because the floor operators read them directly.
const (
	proofReasonMissingReading = "校样缺少位置读数：操作侧、中间、传动侧必须全部录入后才能接收"
	proofReasonTampered       = "复核后校样读数被改动，原判定已失效，需要重新测量与复核"
)

// proofReadingSet groups the three sheet-position ΔE readings.
type proofReadingSet struct {
	operation *float64
	center    *float64
	drive     *float64
}

func readingsFromProof(p *model.ColorProof) proofReadingSet {
	return proofReadingSet{operation: p.OperationSide, center: p.Center, drive: p.DriveSide}
}

func readingsFromDTO(o, c, d *float64) proofReadingSet {
	return proofReadingSet{operation: o, center: c, drive: d}
}

func (r proofReadingSet) complete() bool {
	return r.operation != nil && r.center != nil && r.drive != nil
}

// worst returns the largest ΔE reading and its position label.
func (r proofReadingSet) worst() (float64, string) {
	type candidate struct {
		value float64
		pos   string
		ok    bool
	}
	candidates := []candidate{
		{pos: model.ProofPositionOperation, ok: r.operation != nil},
		{pos: model.ProofPositionCenter, ok: r.center != nil},
		{pos: model.ProofPositionDrive, ok: r.drive != nil},
	}
	if r.operation != nil {
		candidates[0].value = *r.operation
	}
	if r.center != nil {
		candidates[1].value = *r.center
	}
	if r.drive != nil {
		candidates[2].value = *r.drive
	}
	worst := math.NaN()
	position := ""
	for _, item := range candidates {
		if !item.ok {
			continue
		}
		if math.IsNaN(worst) || item.value > worst {
			worst = item.value
			position = item.pos
		}
	}
	if math.IsNaN(worst) {
		return 0, ""
	}
	return worst, position
}

// applyReadings copies the three readings onto the proof and refreshes the
// derived worst value / position and the generic metric mirror.
func applyReadings(p *model.ColorProof, r proofReadingSet) {
	p.OperationSide = r.operation
	p.Center = r.center
	p.DriveSide = r.drive
	worst, position := r.worst()
	p.WorstPosition = position
	p.MetricValue = worst
}

// proofOverToleranceReason describes a worst-position exceedance.
func proofOverToleranceReason(position string, worst, tolerance float64) string {
	return fmt.Sprintf("校样最差位置（%s）ΔE %.2f 超过批次允许范围 %.2f，批次停留在校样阶段",
		proofPositionLabel(position), worst, tolerance)
}

func proofPositionLabel(position string) string {
	switch position {
	case model.ProofPositionOperation:
		return "操作侧"
	case model.ProofPositionCenter:
		return "中间"
	case model.ProofPositionDrive:
		return "传动侧"
	default:
		return position
	}
}

// readingsChanged compares the three positions between two rounds.
func readingsChanged(prev, next proofReadingSet) bool {
	return !sameFloat(prev.operation, next.operation) ||
		!sameFloat(prev.center, next.center) ||
		!sameFloat(prev.drive, next.drive)
}

func sameFloat(a, b *float64) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// activeJudgment returns the current judgment for a loaded proof.
func activeJudgment(p model.ColorProof) *model.ColorProofJudgment {
	for i := range p.Judgments {
		if p.Judgments[i].Active {
			return &p.Judgments[i]
		}
	}
	return nil
}

// newPendingJudgment snapshots a (re)measurement round awaiting review.
func newPendingJudgment(p *model.ColorProof, tolerance float64, actor, requestID, reason string) *model.ColorProofJudgment {
	readings := readingsFromProof(p)
	worst, position := readings.worst()
	holdReason := ""
	if !readings.complete() {
		holdReason = proofReasonMissingReading
	}
	return &model.ColorProofJudgment{
		Conclusion:    model.ProofJudgmentPending,
		Status:        p.Status,
		OperationSide: readings.operation,
		Center:        readings.center,
		DriveSide:     readings.drive,
		WorstValue:    worst,
		WorstPosition: position,
		Tolerance:     tolerance,
		HoldReason:    holdReason,
		RunCode:       p.RelatedCode,
		Actor:         actor,
		RequestID:     requestID,
		Reason:        reason,
	}
}
