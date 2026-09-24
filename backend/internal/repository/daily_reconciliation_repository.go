package repository

import (
	"errors"

	"github.com/blueship581/gbinsureapi/internal/model"
	"github.com/blueship581/gbinsureapi/internal/util"
	"gorm.io/gorm"
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

// FindByClientAndDate 按调用方 + 日期查询。
func (r *DailyReconciliationRepository) FindByClientAndDate(clientID uint, date string) (*model.DailyReconciliation, error) {
	var rec model.DailyReconciliation
	if err := r.db.Where("client_id = ? AND reconcile_date = ?", clientID, date).First(&rec).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, util.ErrNotFound
		}
		return nil, err
	}
	return &rec, nil
}

// ListByClient 分页查询某调用方的对账记录（date 非空时仅返回该自然日，用于回查）。
func (r *DailyReconciliationRepository) ListByClient(clientID uint, date string, page, pageSize int) ([]model.DailyReconciliation, int64, error) {
	q := r.db.Model(&model.DailyReconciliation{}).Where("client_id = ?", clientID)
	if date != "" {
		q = q.Where("reconcile_date = ?", date)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.DailyReconciliation
	err := q.Order("reconcile_date desc, id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error
	return items, total, err
}

// Update 更新对账记录。
func (r *DailyReconciliationRepository) Update(rec *model.DailyReconciliation) error {
	return r.db.Save(rec).Error
}
