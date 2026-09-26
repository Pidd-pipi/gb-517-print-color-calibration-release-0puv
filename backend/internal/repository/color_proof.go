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
	CreateWithJudgment(context.Context, *model.ColorProof, *model.ProofJudgment) error
	Update(context.Context, uint, uint, *model.ColorProof) error
	UpdateWithJudgment(context.Context, uint, uint, *model.ColorProof, *model.ProofJudgment) error
	ListByRunCode(context.Context, string) ([]model.ColorProof, error)
	ListWithJudgmentsByRunCode(context.Context, string) ([]model.ColorProof, error)
	Delete(context.Context, uint) error
	CountByStatus(context.Context) (map[string]int64, error)
}

type colorProofRepository struct {
	store *Store[model.ColorProof]
}

func NewColorProofRepository(db *gorm.DB) ColorProofRepository {
	return &colorProofRepository{store: NewStore[model.ColorProof](db)}
}

func (r *colorProofRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.ColorProof], error) {
	return r.store.List(ctx, q)
}
func (r *colorProofRepository) Get(ctx context.Context, id uint) (model.ColorProof, error) {
	var item model.ColorProof
	err := r.store.db.WithContext(ctx).
		Preload("Judgments", func(db *gorm.DB) *gorm.DB { return db.Order("version DESC") }).
		First(&item, id).Error
	return item, err
}

// CreateWithJudgment persists the proof and its first 判定版本 in one
// transaction so a proof never exists without a judgment chain.
func (r *colorProofRepository) CreateWithJudgment(ctx context.Context, item *model.ColorProof, judgment *model.ProofJudgment) error {
	return r.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Judgments").Create(item).Error; err != nil {
			return err
		}
		judgment.ProofID = item.ID
		return tx.Create(judgment).Error
	})
}

func (r *colorProofRepository) Update(ctx context.Context, id, version uint, item *model.ColorProof) error {
	return r.store.Update(ctx, id, version, item)
}

// UpdateWithJudgment applies the optimistic-locked update and appends the new
// 判定版本 atomically; the unique (proof_id, version) index makes a lost
// update impossible even under concurrent re-measurement.
func (r *colorProofRepository) UpdateWithJudgment(ctx context.Context, id, version uint, item *model.ColorProof, judgment *model.ProofJudgment) error {
	return r.store.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.ColorProof{}).Where("id = ? AND version = ?", id, version).
			Select("*").Omit("id", "code", "created_at", "deleted_at", "Judgments").Updates(item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		judgment.ProofID = id
		return tx.Create(judgment).Error
	})
}

func (r *colorProofRepository) ListByRunCode(ctx context.Context, runCode string) ([]model.ColorProof, error) {
	items := make([]model.ColorProof, 0)
	err := r.store.db.WithContext(ctx).
		Where("related_code = ?", runCode).Order("code ASC").Find(&items).Error
	return items, err
}

func (r *colorProofRepository) ListWithJudgmentsByRunCode(ctx context.Context, runCode string) ([]model.ColorProof, error) {
	items := make([]model.ColorProof, 0)
	err := r.store.db.WithContext(ctx).
		Preload("Judgments", func(db *gorm.DB) *gorm.DB { return db.Order("version DESC") }).
		Where("related_code = ?", runCode).Order("code ASC").Find(&items).Error
	return items, err
}

func (r *colorProofRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *colorProofRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}
