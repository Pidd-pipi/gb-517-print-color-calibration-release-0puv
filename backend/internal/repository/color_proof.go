package repository

import (
	"context"

	"github.com/blueship581/print-color-calibration-release/backend/internal/dto"
	"github.com/blueship581/print-color-calibration-release/backend/internal/model"
	"gorm.io/gorm"
)

// ColorProofRepository owns all persistence operations for 色彩校样.
type ColorProofRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.ColorProof], error)
	Get(context.Context, uint) (model.ColorProof, error)
	Create(context.Context, *model.ColorProof, *model.ColorProofJudgment) error
	UpdateWithJudgment(context.Context, uint, uint, *model.ColorProof, *model.ColorProofJudgment) error
	// SetActiveJudgment marks the active judgment conclusion/hold reason after review.
	SetActiveJudgment(context.Context, uint, string, string) error
	// ReviewUpdate persists a proof status change and updates the active
	// judgment conclusion in one transaction.
	ReviewUpdate(context.Context, uint, uint, *model.ColorProof, string, string) error
	Update(context.Context, uint, uint, *model.ColorProof) error
	Delete(context.Context, uint) error
	CountByStatus(context.Context) (map[string]int64, error)
}

type colorProofRepository struct {
	store *Store[model.ColorProof]
	db    *gorm.DB
}

func NewColorProofRepository(db *gorm.DB) ColorProofRepository {
	return &colorProofRepository{store: NewStore[model.ColorProof](db), db: db}
}

func (r *colorProofRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.ColorProof], error) {
	return r.store.List(ctx, q)
}

func (r *colorProofRepository) Get(ctx context.Context, id uint) (model.ColorProof, error) {
	var item model.ColorProof
	err := r.db.WithContext(ctx).
		Preload("Judgments", func(db *gorm.DB) *gorm.DB { return db.Order("judgment_no DESC") }).
		First(&item, id).Error
	return item, err
}

func (r *colorProofRepository) Create(ctx context.Context, item *model.ColorProof, judgment *model.ColorProofJudgment) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Judgments").Create(item).Error; err != nil {
			return err
		}
		judgment.ColorProofID = item.ID
		judgment.JudgmentNo = 1
		judgment.Active = true
		return tx.Create(judgment).Error
	})
}

// UpdateWithJudgment persists changed readings and appends a fresh judgment
// version in one transaction: previous judgments are marked inactive so the
// old conclusions remain in history while the new one becomes the active
// result used by the batch.
func (r *colorProofRepository) UpdateWithJudgment(ctx context.Context, id, version uint, item *model.ColorProof, judgment *model.ColorProofJudgment) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.ColorProof{}).Where("id = ? AND version = ?", id, version).
			Select("*").Omit("id", "code", "created_at", "deleted_at", "Judgments").Updates(item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		var nextNo uint
		if err := tx.Model(&model.ColorProofJudgment{}).Where("color_proof_id = ?", id).
			Select("COALESCE(MAX(judgment_no), 0) + 1").Scan(&nextNo).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ColorProofJudgment{}).Where("color_proof_id = ?", id).
			Update("active", false).Error; err != nil {
			return err
		}
		judgment.ColorProofID = id
		judgment.JudgmentNo = nextNo
		judgment.Active = true
		return tx.Create(judgment).Error
	})
}

// SetActiveJudgment updates the conclusion (and optional hold reason) of the
// currently active judgment without touching its measurement snapshot.
func (r *colorProofRepository) SetActiveJudgment(ctx context.Context, proofID uint, conclusion, holdReason string) error {
	return r.db.WithContext(ctx).Model(&model.ColorProofJudgment{}).
		Where("color_proof_id = ? AND active = ?", proofID, true).
		Updates(map[string]any{"conclusion": conclusion, "hold_reason": holdReason}).Error
}

func (r *colorProofRepository) Update(ctx context.Context, id, version uint, item *model.ColorProof) error {
	return r.store.Update(ctx, id, version, item)
}

// ReviewUpdate persists a review transition and records its conclusion on the
// active judgment snapshot in the same transaction.
func (r *colorProofRepository) ReviewUpdate(ctx context.Context, id, expectedVersion uint, item *model.ColorProof, conclusion, holdReason string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.ColorProof{}).Where("id = ? AND version = ?", id, expectedVersion).
			Select("*").Omit("id", "code", "created_at", "deleted_at", "Judgments").Updates(item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		return tx.Model(&model.ColorProofJudgment{}).
			Where("color_proof_id = ? AND active = ?", id, true).
			Updates(map[string]any{"conclusion": conclusion, "hold_reason": holdReason}).Error
	})
}

func (r *colorProofRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}

func (r *colorProofRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}
