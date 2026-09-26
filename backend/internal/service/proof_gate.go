package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/blueship581/print-color-calibration-release/backend/internal/constants"
	"github.com/blueship581/print-color-calibration-release/backend/internal/model"
	"github.com/blueship581/print-color-calibration-release/backend/internal/repository"
)

// proofEvaluation is the computed outcome of one 判定版本 before it is
// appended to the judgment chain.
type proofEvaluation struct {
	result    string
	reason    string
	worst     float64
	worstPos  string
	overLimit []string
}

var proofPositionLabels = map[string]string{
	"operator": "操作侧",
	"middle":   "中间",
	"drive":    "传动侧",
}

func proofReadings(proof *model.ColorProof) map[string]*float64 {
	return map[string]*float64{
		"operator": proof.ReadingOperator,
		"middle":   proof.ReadingMiddle,
		"drive":    proof.ReadingDrive,
	}
}

// evaluateProofJudgment judges a proof by its worst 幅面 position instead of
// an averaged value. Missing positions, an over-limit worst value, or a
// reading change after review each produce a non-pass conclusion, and every
// conclusion stays reproducible from the stored judgment versions.
func evaluateProofJudgment(readings map[string]*float64, tolerance float64, hasLimit, tampered bool) proofEvaluation {
	worst := 0.0
	worstPos := ""
	overLimit := make([]string, 0, len(constants.ProofPositions))
	missing := make([]string, 0, len(constants.ProofPositions))
	for _, position := range constants.ProofPositions {
		value := readings[position]
		if value == nil {
			missing = append(missing, proofPositionLabels[position])
			continue
		}
		if worstPos == "" || *value > worst {
			worst, worstPos = *value, position
		}
		if hasLimit && *value > tolerance {
			overLimit = append(overLimit, position)
		}
	}
	evaluation := proofEvaluation{worst: worst, worstPos: worstPos, overLimit: overLimit}
	switch {
	case tampered:
		evaluation.result = string(constants.JudgmentTampered)
		evaluation.reason = "复核后读数被改动，需重新测量并复核"
	case len(missing) > 0:
		evaluation.result = string(constants.JudgmentIncomplete)
		evaluation.reason = fmt.Sprintf("缺少%s读数", strings.Join(missing, "、"))
	case hasLimit && len(overLimit) > 0:
		evaluation.result = string(constants.JudgmentFail)
		evaluation.reason = fmt.Sprintf("最差位置（%s）ΔE %.2f 超过批次允许 %.2f", proofPositionLabels[worstPos], worst, tolerance)
	case hasLimit:
		evaluation.result = string(constants.JudgmentPass)
		evaluation.reason = fmt.Sprintf("三位置读数齐全，最差位置（%s）ΔE %.2f 未超批次允许 %.2f", proofPositionLabels[worstPos], worst, tolerance)
	default:
		evaluation.result = string(constants.JudgmentIncomplete)
		evaluation.reason = "未关联批次或批次未设定允许色差，无法判定"
	}
	return evaluation
}

// applyEvaluation stamps the conclusion onto the proof's denormalized fields
// and returns the 判定版本 to append. Old versions are never touched.
func applyEvaluation(item *model.ColorProof, evaluation proofEvaluation, tolerance float64, actor, requestID string) model.ProofJudgment {
	item.JudgmentVersion++
	item.JudgmentResult = evaluation.result
	item.JudgmentReason = evaluation.reason
	item.WorstPosition = evaluation.worstPos
	item.OverLimit = strings.Join(evaluation.overLimit, ",")
	item.MetricValue = evaluation.worst
	return model.ProofJudgment{
		Version: item.JudgmentVersion, RunCode: item.RelatedCode,
		ReadingOperator: item.ReadingOperator, ReadingMiddle: item.ReadingMiddle, ReadingDrive: item.ReadingDrive,
		WorstValue: evaluation.worst, WorstPosition: evaluation.worstPos, Tolerance: tolerance,
		OverLimit: item.OverLimit, Result: evaluation.result, Reason: evaluation.reason,
		Actor: actor, RequestID: requestID,
	}
}

// runGateReason returns an empty string when the run may leave 校样阶段;
// otherwise it explains exactly which proof and which position block it. The
// gate re-evaluates the current readings against the run's current 允许范围,
// so a tightened limit takes effect immediately, while the tampered mark is
// carried by the proof's judgment chain until a reviewer re-accepts it.
func runGateReason(run model.PrintRun, proofs []model.ColorProof) string {
	if len(proofs) == 0 {
		return fmt.Sprintf("批次 %s 尚无关联校样，停留在校样阶段", run.Code)
	}
	hasLimit := run.DeltaELimit > 0
	for i := range proofs {
		proof := &proofs[i]
		if proof.JudgmentResult == string(constants.JudgmentTampered) {
			return fmt.Sprintf("校样 %s 复核后读数被改动，需重新测量并复核", proof.Code)
		}
		evaluation := evaluateProofJudgment(proofReadings(proof), run.DeltaELimit, hasLimit, false)
		if evaluation.result != string(constants.JudgmentPass) {
			return fmt.Sprintf("校样 %s %s", proof.Code, evaluation.reason)
		}
		if proof.Status != "accepted" {
			return fmt.Sprintf("校样 %s 尚未复核接收（当前状态 %s）", proof.Code, proof.Status)
		}
	}
	return ""
}

// refreshRunHoldReason recomputes the derived 校样滞留原因 for the run linked
// to runCode. It is best-effort: the authoritative check always re-runs
// inside the proofing -> released transition itself.
func refreshRunHoldReason(ctx context.Context, runs repository.PrintRunRepository, proofs repository.ColorProofRepository, runCode string) {
	runCode = strings.ToUpper(strings.TrimSpace(runCode))
	if runCode == "" {
		return
	}
	run, err := runs.GetByCode(ctx, runCode)
	if err != nil {
		return
	}
	if run.Status != string(constants.RunStateProofing) && run.Status != string(constants.RunStateHold) {
		return
	}
	linked, err := proofs.ListByRunCode(ctx, run.Code)
	if err != nil {
		return
	}
	if reason := runGateReason(run, linked); reason != run.HoldReason {
		_ = runs.UpdateHoldReason(ctx, run.ID, reason)
	}
}
