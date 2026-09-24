package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/blueship581/gbinsureapi/internal/constants"
	"github.com/blueship581/gbinsureapi/internal/model"
	"github.com/blueship581/gbinsureapi/internal/repository"
	"github.com/blueship581/gbinsureapi/internal/util"
)

// dateLayout 对账日期格式（与 util.TodayDate 输出一致）。
const dateLayout = "2006-01-02"

// ReconciliationService 日终对账服务（复用 SettlementOrderRepository）。
type ReconciliationService struct {
	orderRepo *repository.SettlementOrderRepository
	recRepo   *repository.DailyReconciliationRepository
	log       *slog.Logger
}

// NewReconciliationService 构造日终对账服务。
func NewReconciliationService(orderRepo *repository.SettlementOrderRepository, recRepo *repository.DailyReconciliationRepository, log *slog.Logger) *ReconciliationService {
	return &ReconciliationService{orderRepo: orderRepo, recRepo: recRepo, log: log}
}

// Daily 生成/更新指定调用方在指定自然日的对账（date 为空表示今天）。
// 按 (client_id, reconcile_date) 幂等 upsert：重复汇总只覆盖该调用方自己的记录。
func (s *ReconciliationService) Daily(ctx context.Context, clientID uint, date string) (*model.DailyReconciliation, error) {
	if date == "" {
		date = util.TodayDate()
	}
	if _, err := time.Parse(dateLayout, date); err != nil {
		return nil, util.BadRequest(constants.MsgReconcileDateInvalid, err)
	}
	orders, err := s.orderRepo.SettledOnDate(clientID, date)
	if err != nil {
		return nil, util.LogError(s.log, constants.LOG_RECONCILIATION_FAILED, fmt.Errorf("list settled orders: %w", err))
	}
	rec := &model.DailyReconciliation{
		ClientID: clientID, ReconcileDate: date,
		TotalCount: int64(len(orders)),
	}
	for _, o := range orders {
		// 原结算笔数与金额一律保留，已冲正订单也不从总额中剔除
		rec.TotalAmount += o.TotalAmount
		switch o.Status {
		case constants.SettlementSettled:
			rec.SuccessCount++
		case constants.SettlementReversed:
			// 冲正单独给出笔数与金额，再从净额里扣回
			rec.ReversedCount++
			rec.ReversedAmount += o.TotalAmount
		case constants.SettlementFailed:
			rec.FailCount++
		case constants.SettlementPendingManual:
			rec.AbnormalOrders++
		}
	}
	rec.TotalAmount = round2(rec.TotalAmount)
	rec.ReversedAmount = round2(rec.ReversedAmount)
	rec.NetAmount = round2(rec.TotalAmount - rec.ReversedAmount)

	mode := "create"
	if existing, err := s.recRepo.FindByClientAndDate(clientID, date); err == nil {
		rec.ID = existing.ID
		rec.CreatedAt = existing.CreatedAt
		if err := s.recRepo.Update(rec); err != nil {
			return nil, util.LogError(s.log, constants.LOG_RECONCILIATION_FAILED, fmt.Errorf("update reconciliation: %w", err))
		}
		mode = "update"
	} else if err != util.ErrNotFound {
		return nil, util.LogError(s.log, constants.LOG_RECONCILIATION_FAILED, fmt.Errorf("find reconciliation: %w", err))
	} else if err := s.recRepo.Create(rec); err != nil {
		return nil, util.LogError(s.log, constants.LOG_RECONCILIATION_FAILED, fmt.Errorf("create reconciliation: %w", err))
	}
	s.log.InfoContext(ctx, constants.LOG_RECONCILIATION_GENERATED,
		"client_id", clientID, "date", date, "total", rec.TotalCount,
		"reversed", rec.ReversedCount, "net_amount", rec.NetAmount, "mode", mode)
	return rec, nil
}

// List 分页查询指定调用方的对账记录（date 非空时仅返回该自然日，用于回查）。
func (s *ReconciliationService) List(ctx context.Context, clientID uint, date string, page, pageSize int) ([]model.DailyReconciliation, int64, error) {
	return s.recRepo.ListByClient(clientID, date, page, pageSize)
}
