package repository

import (
	"errors"

	"github.com/blueship581/gbinsureapi/internal/model"
	"github.com/blueship581/gbinsureapi/internal/util"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DailyReconciliationRepository 日终对账仓储。
type DailyReconciliationRepository struct{ db *gorm.DB }

// NewDailyReconciliationRepository 构造日终对账仓储。
func NewDailyReconciliationRepository(db *gorm.DB) *DailyReconciliationRepository {
	return &DailyReconciliationRepository{db: db}
}

// Create 创建对账记录。
func (r *DailyReconciliationRepository) Create(rec *model.DailyReconciliation) error {
	return r.db.Create(rec).Error
}

// FindByClientDate 按调用方 + 自然日查询本调用方的对账记录。
func (r *DailyReconciliationRepository) FindByClientDate(clientID uint, date string) (*model.DailyReconciliation, error) {
	var rec model.DailyReconciliation
	if err := r.db.Where("client_id = ? AND reconcile_date = ?", clientID, date).First(&rec).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, util.ErrNotFound
		}
		return nil, err
	}
	return &rec, nil
}

// List 按调用方分页查询（只返回登录调用方自己的对账记录），可指定自然日回查。
func (r *DailyReconciliationRepository) List(clientID uint, date string, page, pageSize int) ([]model.DailyReconciliation, int64, error) {
	q := r.db.Model(&model.DailyReconciliation{}).Where("client_id = ?", clientID)
	if date != "" {
		q = q.Where("reconcile_date = ?", date)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.DailyReconciliation
	err := r.db.Model(&model.DailyReconciliation{}).Where("client_id = ?", clientID).
		Scopes(dateScope(date)).
		Order("reconcile_date desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

// Upsert 按 (client_id, reconcile_date) 幂等写入：重复汇总只覆盖本调用方自己的记录。
func (r *DailyReconciliationRepository) Upsert(rec *model.DailyReconciliation) error {
	return r.db.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "client_id"}, {Name: "reconcile_date"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"total_count", "total_amount", "success_count", "fail_count", "abnormal_orders",
			"reversed_count", "reversed_amount", "net_amount", "updated_at",
		}),
	}).Create(rec).Error
}

func dateScope(date string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if date != "" {
			return db.Where("reconcile_date = ?", date)
		}
		return db
	}
}
