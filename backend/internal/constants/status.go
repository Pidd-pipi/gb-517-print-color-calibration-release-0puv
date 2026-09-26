package constants

// Shared status values are mirrored in frontend/src/types/status.ts. Keeping
// the lists explicit makes state-machine drift visible during code review.

type RunState string

const (
	RunStateSetup    RunState = "setup"
	RunStatePrinting RunState = "printing"
	RunStateProofing RunState = "proofing"
	RunStateHold     RunState = "hold"
	RunStateReleased RunState = "released"
)

var AllRunState = []string{"setup", "printing", "proofing", "hold", "released"}

type DecisionType string

const (
	DecisionTypeRelease    DecisionType = "release"
	DecisionTypeRework     DecisionType = "rework"
	DecisionTypeQuarantine DecisionType = "quarantine"
)

var AllDecisionType = []string{"release", "rework", "quarantine"}

// JudgmentResult values describe the conclusion of one 校样判定版本. The
// active judgment of every proof linked to a run gates the proofing ->
// released transition, so the values are shared exactly like RunState.
type JudgmentResult string

const (
	JudgmentPass       JudgmentResult = "pass"
	JudgmentFail       JudgmentResult = "fail"
	JudgmentIncomplete JudgmentResult = "incomplete"
	JudgmentTampered   JudgmentResult = "tampered"
)

var AllJudgmentResult = []string{"pass", "fail", "incomplete", "tampered"}

// ProofPositions are the three fixed 幅面 measurement positions recorded on
// every colour proof: 操作侧 / 中间 / 传动侧.
var ProofPositions = []string{"operator", "middle", "drive"}

var PressUnitTransitions = map[string]map[string]bool{
	"ready":       {"setup": true, "printing": true},
	"setup":       {"printing": true, "maintenance": true, "ready": true},
	"printing":    {"maintenance": true, "setup": true},
	"maintenance": {"printing": true},
}

var PrintRunTransitions = map[string]map[string]bool{
	"setup":    {"printing": true},
	"printing": {"proofing": true, "hold": true, "setup": true},
	"proofing": {"hold": true, "released": true, "printing": true},
	"hold":     {"proofing": true},
	"released": {"hold": true},
}

var ColorProofTransitions = map[string]map[string]bool{
	"captured": {"review": true},
	"review":   {"accepted": true, "rejected": true, "captured": true},
	"accepted": {"review": true},
	"rejected": {"review": true},
}

var ReleaseDecisionTransitions = map[string]map[string]bool{
	"draft":      {"release": true, "rework": true},
	"release":    {"rework": true, "quarantine": true, "draft": true},
	"rework":     {"quarantine": true, "release": true},
	"quarantine": {"rework": true},
}

func CanTransition(graph map[string]map[string]bool, from, to string) bool {
	targets, exists := graph[from]
	return exists && targets[to]
}
