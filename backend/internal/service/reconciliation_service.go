package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/blueship581/gbinsureapi/internal/constants"
	"github.com/blueship581/gbinsureapi/internal/model"
	"github.com/blueship581/gbinsureapi/internal/repository"
	"github.com/blueship581/gbinsureapi/internal/util"
)

// ReconciliationService 日终对账服务（复用 SettlementOrderRepository）。
// 汇总结果按调用方 + 自然日分别保存，重复汇总只覆盖本调用方自己的记录。
type ReconciliationService struct {
	orderRepo *repository.SettlementOrderRepository
	recRepo   *repository.DailyReconciliationRepository
	log       *slog.Logger
}

// NewReconciliationService 构造日终对账服务。
func NewReconciliationService(orderRepo *repository.SettlementOrderRepository, recRepo *repository.DailyReconciliationRepository, log *slog.Logger) *ReconciliationService {
	return &ReconciliationService{orderRepo: orderRepo, recRepo: recRepo, log: log}
}

// Daily 生成/更新某调用方某自然日的对账（幂等 upsert，返回最新汇总）。
// date 为空时默认今天；已冲正订单保留原结算笔数与原金额，另计冲正笔数/金额并从净额扣回。
func (s *ReconciliationService) Daily(ctx context.Context, clientID uint, date string) (*model.DailyReconciliation, error) {
	rawDate := date
	date, err := util.NormalizeDate(date)
	if err != nil {
		return nil, util.BadRequest(constants.MsgReconDateInvalid, fmt.Errorf("DailyReconciliation[client=%d] date=%q: %w", clientID, rawDate, err))
	}
	start, end, err := util.DayRange(date)
	if err != nil {
		return nil, util.BadRequest(constants.MsgReconDateInvalid, fmt.Errorf("DailyReconciliation[client=%d] date=%q: %w", clientID, date, err))
	}
	orders, err := s.orderRepo.FindSettledOnDate(clientID, start, end)
	if err != nil {
		return nil, util.LogError(s.log, constants.LOG_RECONCILIATION_FAILED, fmt.Errorf("DailyReconciliation[client=%d] list orders on %s: %w", clientID, date, err))
	}

	rec := s.summarize(clientID, date, orders)
	mode := "create"
	if existing, err := s.recRepo.FindByClientDate(clientID, date); err == nil {
		rec.ID = existing.ID
		mode = "update"
	} else if !errors.Is(err, util.ErrNotFound) {
		return nil, util.LogError(s.log, constants.LOG_RECONCILIATION_FAILED, fmt.Errorf("DailyReconciliation[client=%d] find %s: %w", clientID, date, err))
	}
	if err := s.recRepo.Upsert(rec); err != nil {
		return nil, util.LogError(s.log, constants.LOG_RECONCILIATION_FAILED, fmt.Errorf("DailyReconciliation[client=%d] upsert %s: %w", clientID, date, err))
	}
	s.log.InfoContext(ctx, constants.LOG_RECONCILIATION_GENERATED,
		"client_id", clientID, "date", date, "mode", mode,
		"total_count", rec.TotalCount, "total_amount", rec.TotalAmount,
		"reversed_count", rec.ReversedCount, "reversed_amount", rec.ReversedAmount,
		"net_amount", rec.NetAmount)
	return rec, nil
}

// summarize 汇总某调用方某自然日已结算订单：
//   - total_count/total_amount：保留全部原结算笔数与原金额（含已冲正）
//   - reversed_count/reversed_amount：当日冲正笔数与被冲正的原结算金额
//   - net_amount：净额 = 原结算总金额 - 冲正金额
//   - success/fail/abnormal：按结算状态分类（reversed 单独统计，不计入 success）
func (s *ReconciliationService) summarize(clientID uint, date string, orders []model.SettlementOrder) *model.DailyReconciliation {
	rec := &model.DailyReconciliation{ClientID: clientID, ReconcileDate: date}
	for _, o := range orders {
		rec.TotalCount++
		rec.TotalAmount += o.TotalAmount
		switch o.Status {
		case constants.SettlementSettled:
			rec.SuccessCount++
		case constants.SettlementFailed:
			rec.FailCount++
		case constants.SettlementPendingManual:
			rec.AbnormalOrders++
		case constants.SettlementReversed:
			rec.ReversedCount++
			rec.ReversedAmount += o.TotalAmount
		}
	}
	rec.TotalAmount = round2(rec.TotalAmount)
	rec.ReversedAmount = round2(rec.ReversedAmount)
	rec.NetAmount = round2(rec.TotalAmount - rec.ReversedAmount)
	return rec
}

// List 分页查询登录调用方自己的对账记录，可指定自然日回查。
func (s *ReconciliationService) List(ctx context.Context, clientID uint, date string, page, pageSize int) ([]model.DailyReconciliation, int64, error) {
	rawDate := date
	date, err := util.NormalizeDate(date)
	if err != nil {
		return nil, 0, util.BadRequest(constants.MsgReconDateInvalid, fmt.Errorf("DailyReconciliation[client=%d] date=%q: %w", clientID, rawDate, err))
	}
	items, total, err := s.recRepo.List(clientID, date, page, pageSize)
	if err != nil {
		return nil, 0, util.LogError(s.log, constants.LOG_RECONCILIATION_FAILED, fmt.Errorf("DailyReconciliation[client=%d] list %s: %w", clientID, date, err))
	}
	return items, total, nil
}
