package dto

import "time"

// CreateColorProof is the public write contract for 色彩校样. Status is deliberately
// omitted so callers cannot bypass the service state machine. The three
// position readings are optional at capture time (a strip may still be
// measured), but a proof missing any of them cannot be accepted in review.
type CreateColorProof struct {
	Code          string    `json:"code" binding:"required,min=2,max=64"`
	Name          string    `json:"name" binding:"required,min=2,max=160"`
	Description   string    `json:"description" binding:"max=1000"`
	Facility      string    `json:"facility" binding:"required,max=120"`
	Owner         string    `json:"owner" binding:"required,max=120"`
	Category      string    `json:"category" binding:"required,max=80"`
	RiskLevel     string    `json:"riskLevel" binding:"required,oneof=low medium high critical"`
	MetricValue   float64   `json:"metricValue"`
	MetricUnit    string    `json:"metricUnit" binding:"max=24"`
	EffectiveAt   time.Time `json:"effectiveAt" binding:"required"`
	Evidence      string    `json:"evidence" binding:"max=2000"`
	RelatedCode   string    `json:"relatedCode" binding:"max=64"`
	OperationSide *float64  `json:"operationSide"`
	Center        *float64  `json:"center"`
	DriveSide     *float64  `json:"driveSide"`
}

type UpdateColorProof struct {
	ExpectedVersion uint      `json:"expectedVersion" binding:"required"`
	Name            string    `json:"name" binding:"required,min=2,max=160"`
	Description     string    `json:"description" binding:"max=1000"`
	Facility        string    `json:"facility" binding:"required,max=120"`
	Owner           string    `json:"owner" binding:"required,max=120"`
	Category        string    `json:"category" binding:"required,max=80"`
	RiskLevel       string    `json:"riskLevel" binding:"required,oneof=low medium high critical"`
	MetricValue     float64   `json:"metricValue"`
	MetricUnit      string    `json:"metricUnit" binding:"max=24"`
	EffectiveAt     time.Time `json:"effectiveAt" binding:"required"`
	Evidence        string    `json:"evidence" binding:"max=2000"`
	RelatedCode     string    `json:"relatedCode" binding:"max=64"`
	OperationSide   *float64  `json:"operationSide"`
	Center          *float64  `json:"center"`
	DriveSide       *float64  `json:"driveSide"`
}
