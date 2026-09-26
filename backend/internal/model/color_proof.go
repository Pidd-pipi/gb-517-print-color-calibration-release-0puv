package model

import "time"

// Reading positions measured across the sheet width. 校样 must sample all
// three positions so end-to-end colour cast cannot be averaged away.
const (
	ProofPositionOperation = "operation" // 操作侧
	ProofPositionCenter    = "center"    // 中间
	ProofPositionDrive     = "drive"     // 传动侧
)

// ProofReading* are lifecycle markers for each immutable judgment version.
const (
	ProofJudgmentPending  = "pending"  // 待复核
	ProofJudgmentAccepted = "accepted" // 复核通过（按最差位置判定）
	ProofJudgmentRejected = "rejected" // 复核不通过
)

// ColorProof models 色彩校样 as an independently versioned aggregate. Each
// proof carries three ΔE readings sampled at the operation side, sheet centre
// and drive side; MetricValue mirrors the worst (largest) of the three so the
// generic metric views keep working. Judgment history is append-only: every
// (re)measurement or review decision produces a new ColorProofJudgment row and
// older conclusions remain visible.
type ColorProof struct {
	BaseModel
	Facility    string    `json:"facility" gorm:"size:120;index"`
	Owner       string    `json:"owner" gorm:"size:120;index"`
	Category    string    `json:"category" gorm:"size:80;index"`
	RiskLevel   string    `json:"riskLevel" gorm:"size:32;index"`
	MetricValue float64   `json:"metricValue"`
	MetricUnit  string    `json:"metricUnit" gorm:"size:24"`
	EffectiveAt time.Time `json:"effectiveAt"`
	Evidence    string    `json:"evidence" gorm:"size:2000"`
	RelatedCode string    `json:"relatedCode" gorm:"size:64;index"`

	// OperationSide/Center/DriveSide are the ΔE readings at the three sheet
	// positions. nil means the position has not been recorded yet; a proof
	// missing any position cannot pass review and holds the batch at proofing.
	OperationSide *float64 `json:"operationSide"`
	Center        *float64 `json:"center"`
	DriveSide     *float64 `json:"driveSide"`
	WorstPosition string   `json:"worstPosition" gorm:"size:16"`

	Judgments []ColorProofJudgment `json:"judgments,omitempty" gorm:"foreignKey:ColorProofID"`
}

func (item *ColorProof) GetBase() *BaseModel { return &item.BaseModel }

func (item ColorProof) TableName() string { return "color_proofs" }

var ColorProofInitialStatus = "captured"

// ReadingsComplete reports whether all three sheet positions were measured.
func (item *ColorProof) ReadingsComplete() bool {
	return item.OperationSide != nil && item.Center != nil && item.DriveSide != nil
}

// ColorProofJudgment is an append-only snapshot of one measurement / review
// round. Only the latest row per proof has Active = true; superseded rows keep
// the old conclusion so the floor can see which result the batch actually uses.
type ColorProofJudgment struct {
	ID            uint      `json:"id" gorm:"primaryKey"`
	ColorProofID  uint      `json:"colorProofId" gorm:"not null;uniqueIndex:idx_color_proof_judgment"`
	JudgmentNo    uint      `json:"judgmentNo" gorm:"not null;uniqueIndex:idx_color_proof_judgment"`
	Active        bool      `json:"active" gorm:"not null;index"`
	Conclusion    string    `json:"conclusion" gorm:"size:24;not null"`
	Status        string    `json:"status" gorm:"size:40;not null"`
	OperationSide *float64  `json:"operationSide"`
	Center        *float64  `json:"center"`
	DriveSide     *float64  `json:"driveSide"`
	WorstValue    float64   `json:"worstValue"`
	WorstPosition string    `json:"worstPosition" gorm:"size:16"`
	Tolerance     float64   `json:"tolerance"`
	HoldReason    string    `json:"holdReason" gorm:"size:500"`
	RunCode       string    `json:"runCode" gorm:"size:64"`
	Actor         string    `json:"actor" gorm:"size:80;not null"`
	RequestID     string    `json:"requestId" gorm:"size:80;not null"`
	Reason        string    `json:"reason" gorm:"size:500;not null"`
	CreatedAt     time.Time `json:"createdAt"`
}
