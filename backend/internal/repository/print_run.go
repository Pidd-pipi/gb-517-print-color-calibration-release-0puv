package repository

import (
	"context"
	"strings"

	"github.com/blueship581/print-color-calibration-release/backend/internal/dto"
	"github.com/blueship581/print-color-calibration-release/backend/internal/model"
	"gorm.io/gorm"
)

// PrintRunRepository owns all persistence operations for 印刷批次.
type PrintRunRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.PrintRun], error)
	Get(context.Context, uint) (model.PrintRun, error)
	FindByCode(context.Context, string) (model.PrintRun, error)
	CreateVersioned(context.Context, *model.PrintRun, string, string, string) error
	UpdateVersioned(context.Context, uint, uint, *model.PrintRun, string, string, string) error
	// HoldAtProofing parks a proofing/held run on hold with the reason reported
	// by the proof gate (missing reading, worst position over tolerance, or
	// readings changed after review). Returns the run ID and true when parked.
	HoldAtProofing(ctx context.Context, code, holdReason, actor, requestID, reason string) (uint, bool, error)
	// ResumeProofing clears the proof-gate hold after a re-measurement passes.
	ResumeProofing(ctx context.Context, code, actor, requestID, reason string) (uint, error)
	Delete(context.Context, uint) error
	CountByStatus(context.Context) (map[string]int64, error)
}

type printRunRepository struct {
	store *Store[model.PrintRun]
	db    *gorm.DB
}

func NewPrintRunRepository(db *gorm.DB) PrintRunRepository {
	return &printRunRepository{store: NewStore[model.PrintRun](db), db: db}
}

func (r *printRunRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.PrintRun], error) {
	return r.store.List(ctx, q)
}
func (r *printRunRepository) Get(ctx context.Context, id uint) (model.PrintRun, error) {
	var item model.PrintRun
	err := r.db.WithContext(ctx).
		Preload("Revisions", func(db *gorm.DB) *gorm.DB { return db.Order("version DESC") }).
		First(&item, id).Error
	return item, err
}

func (r *printRunRepository) FindByCode(ctx context.Context, code string) (model.PrintRun, error) {
	var item model.PrintRun
	err := r.db.WithContext(ctx).
		Where("UPPER(code) = ?", strings.ToUpper(strings.TrimSpace(code))).
		First(&item).Error
	return item, err
}

func (r *printRunRepository) CreateVersioned(ctx context.Context, item *model.PrintRun, actor, requestID, reason string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Revisions").Create(item).Error; err != nil {
			return err
		}
		return tx.Create(printRunRevision(item, actor, requestID, reason)).Error
	})
}
func (r *printRunRepository) UpdateVersioned(ctx context.Context, id, version uint, item *model.PrintRun, actor, requestID, reason string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.PrintRun{}).Where("id = ? AND version = ?", id, version).
			Select("*").Omit("id", "code", "created_at", "deleted_at", "Revisions").Updates(item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		return tx.Create(printRunRevision(item, actor, requestID, reason)).Error
	})
}

// moveAtProofing is the shared transaction used by the proof gate: it bumps the
// optimistic version and appends an immutable revision recording the reason.
func (r *printRunRepository) moveAtProofing(ctx context.Context, code, status, holdReason, actor, requestID, reason string) (model.PrintRun, error) {
	var run model.PrintRun
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("UPPER(code) = ?", strings.ToUpper(strings.TrimSpace(code))).First(&run).Error; err != nil {
			return err
		}
		before := run.Version
		run.Status = status
		run.HoldReason = holdReason
		run.Version = before + 1
		result := tx.Model(&model.PrintRun{}).Where("id = ? AND version = ?", run.ID, before).
			Select("*").Omit("id", "code", "created_at", "deleted_at", "Revisions").Updates(&run)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		return tx.Create(printRunRevision(&run, actor, requestID, reason)).Error
	})
	return run, err
}

// HoldAtProofing parks a proofing/held run on hold with the reason reported by
// the proof gate (missing reading, worst position over tolerance, or readings
// changed after review). Runs that never reached proofing are left untouched.
func (r *printRunRepository) HoldAtProofing(ctx context.Context, code, holdReason, actor, requestID, reason string) (uint, bool, error) {
	var existing model.PrintRun
	findErr := r.db.WithContext(ctx).
		Where("UPPER(code) = ?", strings.ToUpper(strings.TrimSpace(code))).First(&existing).Error
	if findErr != nil {
		if findErr == gorm.ErrRecordNotFound {
			return 0, false, nil
		}
		return 0, false, findErr
	}
	if existing.Status != "proofing" && existing.Status != "hold" {
		return 0, false, nil
	}
	run, err := r.moveAtProofing(ctx, code, "hold", holdReason, actor, requestID, reason)
	if err != nil {
		return 0, false, err
	}
	return run.ID, true, nil
}

// ResumeProofing clears the proof-gate hold after a re-measurement passes.
// Runs outside the proofing/hold gate are left untouched.
func (r *printRunRepository) ResumeProofing(ctx context.Context, code, actor, requestID, reason string) (uint, error) {
	var existing model.PrintRun
	findErr := r.db.WithContext(ctx).
		Where("UPPER(code) = ?", strings.ToUpper(strings.TrimSpace(code))).First(&existing).Error
	if findErr != nil {
		if findErr == gorm.ErrRecordNotFound {
			return 0, nil
		}
		return 0, findErr
	}
	if existing.Status != "proofing" && existing.Status != "hold" {
		return 0, nil
	}
	run, err := r.moveAtProofing(ctx, code, "proofing", "", actor, requestID, reason)
	if err != nil {
		return 0, err
	}
	return run.ID, nil
}

func printRunRevision(item *model.PrintRun, actor, requestID, reason string) *model.PrintRunRevision {
	return &model.PrintRunRevision{
		PrintRunID: item.ID, Version: item.Version, Status: item.Status, Name: item.Name,
		Facility: item.Facility, Owner: item.Owner, Category: item.Category,
		RiskLevel: item.RiskLevel, MetricValue: item.MetricValue, MetricUnit: item.MetricUnit,
		Evidence: item.Evidence, RelatedCode: item.RelatedCode,
		ColorTolerance: item.ColorTolerance, HoldReason: item.HoldReason,
		Actor: actor, RequestID: requestID, Reason: reason,
	}
}
func (r *printRunRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *printRunRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}
