package repository

import (
	"errors"

	"github.com/blueship581/gbinsureapi/internal/model"
	"github.com/blueship581/gbinsureapi/internal/util"
	"gorm.io/gorm"
)

// SettlementOrderRepository 结算单仓储。
type SettlementOrderRepository struct{ db *gorm.DB }

// NewSettlementOrderRepository 构造结算单仓储。
func NewSettlementOrderRepository(db *gorm.DB) *SettlementOrderRepository {
	return &SettlementOrderRepository{db: db}
}

// Create 创建结算单。
func (r *SettlementOrderRepository) Create(order *model.SettlementOrder) error {
	return r.db.Create(order).Error
}

// FindByNo 按结算单号查询。
func (r *SettlementOrderRepository) FindByNo(no string) (*model.SettlementOrder, error) {
	var order model.SettlementOrder
	if err := r.db.Where("settlement_no = ?", no).First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, util.ErrNotFound
		}
		return nil, err
	}
	return &order, nil
}

// ExistsByNo 结算单号是否存在。
func (r *SettlementOrderRepository) ExistsByNo(no string) (bool, error) {
	var count int64
	err := r.db.Model(&model.SettlementOrder{}).Where("settlement_no = ?", no).Count(&count).Error
	return count > 0, err
}

// Update 更新结算单。
func (r *SettlementOrderRepository) Update(order *model.SettlementOrder) error { return r.db.Save(order).Error }

// List 分页查询（固定按调用方隔离，可选状态过滤）。
func (r *SettlementOrderRepository) List(clientID uint, status string, page, pageSize int) ([]model.SettlementOrder, int64, error) {
	q := r.db.Model(&model.SettlementOrder{}).Where("client_id = ?", clientID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var orders []model.SettlementOrder
	err := q.Order("id desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&orders).Error
	return orders, total, err
}

// SettledOnDate 查询某调用方在指定自然日（Asia/Shanghai 日期串）已正式结算的订单。
// 已冲正订单同样按原结算日计入，由服务层按状态汇总。
func (r *SettlementOrderRepository) SettledOnDate(clientID uint, date string) ([]model.SettlementOrder, error) {
	var orders []model.SettlementOrder
	err := r.db.Where("client_id = ? AND date(settled_at) = ?", clientID, date).Find(&orders).Error
	return orders, err
}

// Count 统计。
func (r *SettlementOrderRepository) Count() (int64, error) {
	var count int64
	err := r.db.Model(&model.SettlementOrder{}).Count(&count).Error
	return count, err
}
