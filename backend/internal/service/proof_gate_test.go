package service

import (
	"strings"
	"testing"

	"github.com/blueship581/print-color-calibration-release/backend/internal/model"
)

func readingPtr(value float64) *float64 { return &value }

func readings(operator, middle, drive *float64) map[string]*float64 {
	return map[string]*float64{"operator": operator, "middle": middle, "drive": drive}
}

func TestEvaluateProofJudgment(t *testing.T) {
	t.Run("missing position is incomplete", func(t *testing.T) {
		evaluation := evaluateProofJudgment(readings(readingPtr(1.5), readingPtr(2.0), nil), 3.0, true, false)
		if evaluation.result != "incomplete" || !strings.Contains(evaluation.reason, "传动侧") {
			t.Fatalf("expected incomplete judgment naming 传动侧, got %+v", evaluation)
		}
	})
	t.Run("worst position drives the verdict", func(t *testing.T) {
		evaluation := evaluateProofJudgment(readings(readingPtr(2.1), readingPtr(3.4), readingPtr(2.6)), 3.0, true, false)
		if evaluation.result != "fail" || evaluation.worstPos != "middle" || evaluation.worst != 3.4 {
			t.Fatalf("expected fail on 中间, got %+v", evaluation)
		}
		if len(evaluation.overLimit) != 1 || evaluation.overLimit[0] != "middle" {
			t.Fatalf("expected 中间 as the only over-limit position, got %+v", evaluation.overLimit)
		}
		if !strings.Contains(evaluation.reason, "中间") || !strings.Contains(evaluation.reason, "3.40") {
			t.Fatalf("reason should name the worst position and value, got %q", evaluation.reason)
		}
	})
	t.Run("averaging would hide an over-limit edge", func(t *testing.T) {
		// Mean of 1.0/1.2/4.0 is 2.07 which looks fine; the worst position is not.
		evaluation := evaluateProofJudgment(readings(readingPtr(1.0), readingPtr(1.2), readingPtr(4.0)), 3.0, true, false)
		if evaluation.result != "fail" || evaluation.worstPos != "drive" {
			t.Fatalf("edge position must not be averaged away, got %+v", evaluation)
		}
	})
	t.Run("tampered beats every other signal", func(t *testing.T) {
		evaluation := evaluateProofJudgment(readings(readingPtr(1.0), readingPtr(1.2), readingPtr(1.4)), 3.0, true, true)
		if evaluation.result != "tampered" || !strings.Contains(evaluation.reason, "改动") {
			t.Fatalf("expected tampered judgment, got %+v", evaluation)
		}
	})
	t.Run("complete readings within limit pass", func(t *testing.T) {
		evaluation := evaluateProofJudgment(readings(readingPtr(1.8), readingPtr(2.1), readingPtr(2.4)), 3.0, true, false)
		if evaluation.result != "pass" || evaluation.worstPos != "drive" {
			t.Fatalf("expected pass, got %+v", evaluation)
		}
	})
	t.Run("without a batch limit the proof cannot be judged", func(t *testing.T) {
		evaluation := evaluateProofJudgment(readings(readingPtr(1.0), readingPtr(1.0), readingPtr(1.0)), 0, false, false)
		if evaluation.result != "incomplete" {
			t.Fatalf("expected incomplete without limit, got %+v", evaluation)
		}
	})
}

func gateProof(code, status, judgmentResult string, operator, middle, drive *float64) model.ColorProof {
	item := model.ColorProof{
		ReadingOperator: operator, ReadingMiddle: middle, ReadingDrive: drive,
		JudgmentResult: judgmentResult,
	}
	item.Code = code
	item.Status = status
	return item
}

func TestRunGateReason(t *testing.T) {
	run := model.PrintRun{DeltaELimit: 3.0}
	run.Code = "PR-GATE"

	t.Run("no proofs keeps the run in proofing", func(t *testing.T) {
		if reason := runGateReason(run, nil); !strings.Contains(reason, "尚无关联校样") {
			t.Fatalf("expected missing-proof reason, got %q", reason)
		}
	})
	t.Run("missing position blocks with the position named", func(t *testing.T) {
		proofs := []model.ColorProof{gateProof("CP-1", "accepted", "incomplete", readingPtr(1.0), nil, readingPtr(1.2))}
		if reason := runGateReason(run, proofs); !strings.Contains(reason, "CP-1") || !strings.Contains(reason, "中间") {
			t.Fatalf("expected 中间 missing reason, got %q", reason)
		}
	})
	t.Run("worst value over the batch limit blocks", func(t *testing.T) {
		proofs := []model.ColorProof{gateProof("CP-2", "accepted", "fail", readingPtr(2.2), readingPtr(3.4), readingPtr(2.6))}
		if reason := runGateReason(run, proofs); !strings.Contains(reason, "超过批次允许 3.00") {
			t.Fatalf("expected over-limit reason, got %q", reason)
		}
	})
	t.Run("gate uses the current batch limit", func(t *testing.T) {
		strict := model.PrintRun{DeltaELimit: 2.0}
		strict.Code = "PR-GATE"
		proofs := []model.ColorProof{gateProof("CP-3", "accepted", "pass", readingPtr(1.8), readingPtr(2.1), readingPtr(2.4))}
		if reason := runGateReason(strict, proofs); !strings.Contains(reason, "超过批次允许 2.00") {
			t.Fatalf("tightened limit must block previously passing readings, got %q", reason)
		}
	})
	t.Run("tampered readings block until re-review", func(t *testing.T) {
		proofs := []model.ColorProof{gateProof("CP-4", "accepted", "tampered", readingPtr(1.0), readingPtr(1.1), readingPtr(1.2))}
		if reason := runGateReason(run, proofs); !strings.Contains(reason, "改动") {
			t.Fatalf("expected tamper reason, got %q", reason)
		}
	})
	t.Run("unreviewed proof blocks", func(t *testing.T) {
		proofs := []model.ColorProof{gateProof("CP-5", "review", "pass", readingPtr(1.0), readingPtr(1.1), readingPtr(1.2))}
		if reason := runGateReason(run, proofs); !strings.Contains(reason, "尚未复核接收") {
			t.Fatalf("expected unreviewed reason, got %q", reason)
		}
	})
	t.Run("accepted passing proofs release the gate", func(t *testing.T) {
		proofs := []model.ColorProof{gateProof("CP-6", "accepted", "pass", readingPtr(1.0), readingPtr(1.1), readingPtr(1.2))}
		if reason := runGateReason(run, proofs); reason != "" {
			t.Fatalf("expected open gate, got %q", reason)
		}
	})
}
