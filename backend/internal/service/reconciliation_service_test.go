package service

import (
	"context"
	"testing"
	"time"

	"github.com/blueship581/gbinsureapi/internal/constants"
	"github.com/blueship581/gbinsureapi/internal/model"
	"github.com/blueship581/gbinsureapi/internal/repository"
	"github.com/blueship581/gbinsureapi/internal/util"
)

// TestReconciliationService_Daily_PerClient 不同调用方同一天汇总互不覆盖。
func TestReconciliationService_Daily_PerClient(t *testing.T) {
	db := newTestDB(t)
	clientA, personID := seedData(t, db)

	clientB := model.ApiClient{Name: "第三方药房", ClientType: constants.ClientTypeThirdParty, APIKeyHash: "h", Role: "query", Status: constants.ClientActive, RateLimitQPS: 5}
	if err := db.Create(&clientB).Error; err != nil {
		t.Fatal(err)
	}
	date := util.TodayDate()
	start, _, err := util.DayRange(date)
	if err != nil {
		t.Fatal(err)
	}
	mkOrder := func(clientID uint, no string, amount float64, status string) {
		settled := start.Add(time.Hour)
		o := model.SettlementOrder{
			SettlementNo: no, BatchID: 1, InsuredPersonID: personID, PresettlementID: 1,
			ClientID: clientID, Status: status, TotalAmount: amount, InsurancePayAmount: amount, SettledAt: &settled,
		}
		if err := db.Create(&o).Error; err != nil {
			t.Fatal(err)
		}
	}
	mkOrder(clientA, "GB-A-1", 100, constants.SettlementSettled)
	mkOrder(clientA, "GB-A-2", 400, constants.SettlementSettled)
	mkOrder(clientB.ID, "GB-B-1", 999, constants.SettlementSettled)

	svc := NewReconciliationService(
		repository.NewSettlementOrderRepository(db),
		repository.NewDailyReconciliationRepository(db),
		testLogger(),
	)
	ctx := context.Background()

	recA, err := svc.Daily(ctx, clientA, "")
	if err != nil {
		t.Fatalf("Daily A error = %v", err)
	}
	recB, err := svc.Daily(ctx, clientB.ID, "")
	if err != nil {
		t.Fatalf("Daily B error = %v", err)
	}

	// A 的汇总不被 B 冲掉
	if recA.TotalCount != 2 || recA.TotalAmount != 500 {
		t.Fatalf("A total = (%d, %.2f), want (2, 500)", recA.TotalCount, recA.TotalAmount)
	}
	if recB.TotalCount != 1 || recB.TotalAmount != 999 {
		t.Fatalf("B total = (%d, %.2f), want (1, 999)", recB.TotalCount, recB.TotalAmount)
	}
	if recA.ClientID != clientA || recB.ClientID != clientB.ID {
		t.Fatalf("client scoping broken: A=%d B=%d", recA.ClientID, recB.ClientID)
	}

	// 库中同一自然日应有两条、分属两个调用方
	items, total, err := svc.List(ctx, clientA, "", 1, 20)
	if err != nil {
		t.Fatalf("List A error = %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("List A total=%d len=%d, want 1/1", total, len(items))
	}
	var allCount int64
	if err := db.Model(&model.DailyReconciliation{}).Count(&allCount).Error; err != nil {
		t.Fatal(err)
	}
	if allCount != 2 {
		t.Fatalf("reconciliation rows = %d, want 2 (one per client)", allCount)
	}
}

// TestReconciliationService_Daily_Reversal 已冲正订单保留原笔数与金额，冲正单独统计并扣减净额。
func TestReconciliationService_Daily_Reversal(t *testing.T) {
	db := newTestDB(t)
	clientID, personID := seedData(t, db)
	date := util.TodayDate()
	start, _, err := util.DayRange(date)
	if err != nil {
		t.Fatal(err)
	}
	ts := start.Add(2 * time.Hour)
	orders := []model.SettlementOrder{
		{SettlementNo: "GB-1", BatchID: 1, InsuredPersonID: personID, PresettlementID: 1, ClientID: clientID, Status: constants.SettlementSettled, TotalAmount: 100, SettledAt: &ts},
		{SettlementNo: "GB-2", BatchID: 1, InsuredPersonID: personID, PresettlementID: 1, ClientID: clientID, Status: constants.SettlementReversed, TotalAmount: 300, SettledAt: &ts, ReversedAt: &ts},
		{SettlementNo: "GB-3", BatchID: 1, InsuredPersonID: personID, PresettlementID: 1, ClientID: clientID, Status: constants.SettlementReversed, TotalAmount: 50.5, SettledAt: &ts, ReversedAt: &ts},
	}
	for i := range orders {
		if err := db.Create(&orders[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	svc := NewReconciliationService(
		repository.NewSettlementOrderRepository(db),
		repository.NewDailyReconciliationRepository(db),
		testLogger(),
	)
	rec, err := svc.Daily(context.Background(), clientID, date)
	if err != nil {
		t.Fatalf("Daily error = %v", err)
	}
	if rec.TotalCount != 3 {
		t.Fatalf("TotalCount = %d, want 3（含已冲正）", rec.TotalCount)
	}
	if rec.TotalAmount != 450.5 {
		t.Fatalf("TotalAmount = %.2f, want 450.50（原结算金额保留）", rec.TotalAmount)
	}
	if rec.ReversedCount != 2 {
		t.Fatalf("ReversedCount = %d, want 2", rec.ReversedCount)
	}
	if rec.ReversedAmount != 350.5 {
		t.Fatalf("ReversedAmount = %.2f, want 350.50", rec.ReversedAmount)
	}
	if rec.NetAmount != 100 {
		t.Fatalf("NetAmount = %.2f, want 100.00", rec.NetAmount)
	}
	if rec.SuccessCount != 1 {
		t.Fatalf("SuccessCount = %d, want 1", rec.SuccessCount)
	}
}

// TestReconciliationService_Daily_RepeatOverwritesOwn 重复汇总只覆盖自己的记录（幂等，行数不增加）。
func TestReconciliationService_Daily_RepeatOverwritesOwn(t *testing.T) {
	db := newTestDB(t)
	clientID, personID := seedData(t, db)
	date := util.TodayDate()
	start, _, err := util.DayRange(date)
	if err != nil {
		t.Fatal(err)
	}
	ts := start.Add(3 * time.Hour)
	if err := db.Create(&model.SettlementOrder{SettlementNo: "GB-1", BatchID: 1, InsuredPersonID: personID, PresettlementID: 1, ClientID: clientID, Status: constants.SettlementSettled, TotalAmount: 200, SettledAt: &ts}).Error; err != nil {
		t.Fatal(err)
	}
	recRepo := repository.NewDailyReconciliationRepository(db)
	svc := NewReconciliationService(repository.NewSettlementOrderRepository(db), recRepo, testLogger())
	ctx := context.Background()

	first, err := svc.Daily(ctx, clientID, date)
	if err != nil {
		t.Fatal(err)
	}
	// 追加一笔已冲正单后重新汇总
	if err := db.Create(&model.SettlementOrder{SettlementNo: "GB-2", BatchID: 1, InsuredPersonID: personID, PresettlementID: 1, ClientID: clientID, Status: constants.SettlementReversed, TotalAmount: 80, SettledAt: &ts, ReversedAt: &ts}).Error; err != nil {
		t.Fatal(err)
	}
	second, err := svc.Daily(ctx, clientID, date)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID {
		t.Fatalf("record id changed: %d -> %d（应覆盖同一条记录）", first.ID, second.ID)
	}
	if second.TotalCount != 2 || second.TotalAmount != 280 || second.ReversedCount != 1 || second.NetAmount != 200 {
		t.Fatalf("second summary wrong: %+v", second)
	}
	var count int64
	if err := db.Model(&model.DailyReconciliation{}).Where("client_id = ?", clientID).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rows = %d, want 1（重复汇总不得新增）", count)
	}
}

// TestReconciliationService_Daily_DateLookup 指定日期回查 + 非法日期报错。
func TestReconciliationService_Daily_DateLookup(t *testing.T) {
	db := newTestDB(t)
	clientID, personID := seedData(t, db)
	historyDate := "2026-01-15"
	start, _, err := util.DayRange(historyDate)
	if err != nil {
		t.Fatal(err)
	}
	ts := start.Add(4 * time.Hour)
	if err := db.Create(&model.SettlementOrder{SettlementNo: "GB-OLD", BatchID: 1, InsuredPersonID: personID, PresettlementID: 1, ClientID: clientID, Status: constants.SettlementSettled, TotalAmount: 700, SettledAt: &ts}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewReconciliationService(
		repository.NewSettlementOrderRepository(db),
		repository.NewDailyReconciliationRepository(db),
		testLogger(),
	)
	ctx := context.Background()

	rec, err := svc.Daily(ctx, clientID, historyDate)
	if err != nil {
		t.Fatalf("Daily history error = %v", err)
	}
	if rec.ReconcileDate != historyDate || rec.TotalAmount != 700 || rec.TotalCount != 1 {
		t.Fatalf("history summary wrong: %+v", rec)
	}

	// 列表按日期回查
	items, total, err := svc.List(ctx, clientID, historyDate, 1, 20)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("List by date: items=%v total=%d err=%v", items, total, err)
	}

	// 今天不应混入历史记录
	todayItems, todayTotal, err := svc.List(ctx, clientID, util.TodayDate(), 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if todayTotal != 0 || len(todayItems) != 0 {
		t.Fatalf("today list should be empty: %+v", todayItems)
	}

	// 非法日期
	for _, bad := range []string{"2026-13-01", "2026/01/15", "not-a-date"} {
		if _, err := svc.Daily(ctx, clientID, bad); err == nil {
			t.Fatalf("expected error for date %q", bad)
		}
	}
}
