package model

import "time"

// ColorProof models 色彩校样 as an independently versioned aggregate. Every
// proof carries three 幅面 readings (操作侧/中间/传动侧) instead of a single
// averaged value, and each re-measurement or review appends an immutable
// ProofJudgment so older conclusions stay in history.
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
	// Three-position ΔE readings; a nil pointer means the position 未录.
	ReadingOperator *float64 `json:"readingOperator"`
	ReadingMiddle   *float64 `json:"readingMiddle"`
	ReadingDrive    *float64 `json:"readingDrive"`
	// Snapshot taken when a reviewer accepted/rejected the proof. Any later
	// reading change is detected as 复核后改动 against this snapshot.
	ReviewedOperator *float64 `json:"reviewedOperator"`
	ReviewedMiddle   *float64 `json:"reviewedMiddle"`
	ReviewedDrive    *float64 `json:"reviewedDrive"`
	// Denormalized copy of the active judgment so lists and the batch gate can
	// read the current conclusion without replaying the judgment chain.
	JudgmentVersion uint            `json:"judgmentVersion" gorm:"not null;default:0"`
	JudgmentResult  string          `json:"judgmentResult" gorm:"size:16;index"`
	JudgmentReason  string          `json:"judgmentReason" gorm:"size:500"`
	WorstPosition   string          `json:"worstPosition" gorm:"size:16"`
	OverLimit       string          `json:"overLimit" gorm:"size:64"`
	Judgments       []ProofJudgment `json:"judgments,omitempty" gorm:"foreignKey:ProofID"`
}

func (item *ColorProof) GetBase() *BaseModel { return &item.BaseModel }

func (item ColorProof) TableName() string { return "color_proofs" }

var ColorProofInitialStatus = "captured"

// ProofJudgment is an append-only 判定版本. Re-measurement and every review
// decision create a new version; previous versions are never updated so the
// floor can always see which conclusion a batch currently relies on.
type ProofJudgment struct {
	ID              uint      `json:"id" gorm:"primaryKey"`
	ProofID         uint      `json:"proofId" gorm:"not null;uniqueIndex:idx_proof_judgment"`
	Version         uint      `json:"version" gorm:"not null;uniqueIndex:idx_proof_judgment"`
	RunCode         string    `json:"runCode" gorm:"size:64;index"`
	ReadingOperator *float64  `json:"readingOperator"`
	ReadingMiddle   *float64  `json:"readingMiddle"`
	ReadingDrive    *float64  `json:"readingDrive"`
	WorstValue      float64   `json:"worstValue"`
	WorstPosition   string    `json:"worstPosition" gorm:"size:16"`
	Tolerance       float64   `json:"tolerance"`
	OverLimit       string    `json:"overLimit" gorm:"size:64"`
	Result          string    `json:"result" gorm:"size:16;index"`
	Reason          string    `json:"reason" gorm:"size:500"`
	Actor           string    `json:"actor" gorm:"size:80;not null"`
	RequestID       string    `json:"requestId" gorm:"size:80;not null"`
	CreatedAt       time.Time `json:"createdAt"`
}

func (item ProofJudgment) TableName() string { return "proof_judgments" }
