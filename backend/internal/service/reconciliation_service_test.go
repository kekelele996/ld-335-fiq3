package service

import (
	"context"
	"testing"
	"time"

	"github.com/blueship581/gbinsureapi/internal/constants"
	"github.com/blueship581/gbinsureapi/internal/model"
	"github.com/blueship581/gbinsureapi/internal/repository"
	"github.com/blueship581/gbinsureapi/internal/util"
	"gorm.io/gorm"
)

func settleAt(date string, hour int) *time.Time {
	d, _ := time.ParseInLocation("2006-01-02", date, time.FixedZone("CST", 8*3600))
	t := d.Add(time.Duration(hour) * time.Hour)
	return &t
}

func createReconOrder(t *testing.T, db *gorm.DB, no string, clientID uint, amount float64, status string, date string) {
	t.Helper()
	order := model.SettlementOrder{
		SettlementNo: no, ClientID: clientID, Status: status,
		TotalAmount: amount, InsurancePayAmount: amount, SettledAt: settleAt(date, 10),
	}
	if status == constants.SettlementReversed {
		order.ReversedAt = settleAt(date, 11)
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create order %s: %v", no, err)
	}
}

func TestReconciliationService_DailyIsolatedByClient(t *testing.T) {
	db := newTestDB(t)
	hisID, _ := seedData(t, db)
	// 第二个调用方：第三方药房
	third := model.ApiClient{Name: "第三方药房", ClientType: constants.ClientTypeThirdParty, APIKeyHash: "h", Role: "query", Status: constants.ClientActive, RateLimitQPS: 5}
	if err := db.Create(&third).Error; err != nil {
		t.Fatal(err)
	}
	date := util.TodayDate()
	createReconOrder(t, db, "JS20260101001", hisID, 100, constants.SettlementSettled, date)
	createReconOrder(t, db, "JS20260101002", hisID, 50, constants.SettlementSettled, date)
	createReconOrder(t, db, "JS20260101003", hisID, 30, constants.SettlementReversed, date)
	createReconOrder(t, db, "JS20260101004", third.ID, 200, constants.SettlementSettled, date)

	svc := NewReconciliationService(
		repository.NewSettlementOrderRepository(db),
		repository.NewDailyReconciliationRepository(db), testLogger(),
	)
	ctx := context.Background()

	his, err := svc.Daily(ctx, hisID, "")
	if err != nil {
		t.Fatalf("HIS Daily() error = %v", err)
	}
	// 原结算笔数与金额保留：3 笔 / 180；冲正 1 笔 / 30 单独给出；净额 150
	if his.TotalCount != 3 || his.TotalAmount != 180 {
		t.Fatalf("HIS total = (%d, %v), want (3, 180)", his.TotalCount, his.TotalAmount)
	}
	if his.SuccessCount != 2 {
		t.Fatalf("HIS success = %d, want 2", his.SuccessCount)
	}
	if his.ReversedCount != 1 || his.ReversedAmount != 30 {
		t.Fatalf("HIS reversed = (%d, %v), want (1, 30)", his.ReversedCount, his.ReversedAmount)
	}
	if his.NetAmount != 150 {
		t.Fatalf("HIS net = %v, want 150", his.NetAmount)
	}

	// 药房在 HIS 之后汇总，不得覆盖 HIS 的结果
	pharmacy, err := svc.Daily(ctx, third.ID, "")
	if err != nil {
		t.Fatalf("pharmacy Daily() error = %v", err)
	}
	if pharmacy.TotalCount != 1 || pharmacy.TotalAmount != 200 || pharmacy.NetAmount != 200 {
		t.Fatalf("pharmacy = %+v, want 1 笔 / 200 / 净额 200", pharmacy)
	}

	// 回查 HIS：记录仍是 HIS 自己的
	hisAgain, err := svc.Daily(ctx, hisID, date)
	if err != nil {
		t.Fatalf("HIS Daily(date) error = %v", err)
	}
	if hisAgain.ID != his.ID || hisAgain.TotalCount != 3 || hisAgain.NetAmount != 150 {
		t.Fatalf("HIS record overwritten: got %+v", hisAgain)
	}
}

func TestReconciliationService_RepeatedDailyOnlyOverwritesOwn(t *testing.T) {
	db := newTestDB(t)
	hisID, _ := seedData(t, db)
	date := util.TodayDate()
	svc := NewReconciliationService(
		repository.NewSettlementOrderRepository(db),
		repository.NewDailyReconciliationRepository(db), testLogger(),
	)
	ctx := context.Background()

	first, err := svc.Daily(ctx, hisID, date)
	if err != nil {
		t.Fatalf("Daily() error = %v", err)
	}
	if first.TotalCount != 0 {
		t.Fatalf("empty day count = %d, want 0", first.TotalCount)
	}
	// 当天新增一笔结算后再次汇总：更新同一条记录
	createReconOrder(t, db, "JS20260102001", hisID, 80, constants.SettlementSettled, date)
	second, err := svc.Daily(ctx, hisID, date)
	if err != nil {
		t.Fatalf("Daily() 2nd error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatal("repeated daily should upsert same record")
	}
	if second.TotalCount != 1 || second.TotalAmount != 80 || second.NetAmount != 80 {
		t.Fatalf("second = %+v, want 1 笔 / 80 / 净额 80", second)
	}

	// 记录列表按登录调用方查看，可按日期回查
	items, total, err := svc.List(ctx, hisID, date, 1, 20)
	if err != nil || total != 1 || len(items) != 1 || items[0].ID != first.ID {
		t.Fatalf("List(date) = (%v, %d, %v), want own single record", items, total, err)
	}
	// 指定一个没有数据的日期：列表为空
	if _, otherTotal, err := svc.List(ctx, hisID, "2020-01-01", 1, 20); err != nil || otherTotal != 0 {
		t.Fatalf("List(other date) total = %d, err = %v", otherTotal, err)
	}
}

func TestReconciliationService_HistoricalDateBackfill(t *testing.T) {
	db := newTestDB(t)
	hisID, _ := seedData(t, db)
	svc := NewReconciliationService(
		repository.NewSettlementOrderRepository(db),
		repository.NewDailyReconciliationRepository(db), testLogger(),
	)
	ctx := context.Background()

	// 指定历史日期回查：当日无对账记录时按该日订单生成一条
	past := "2026-01-02"
	createReconOrder(t, db, "JS20260102010", hisID, 120, constants.SettlementSettled, past)
	createReconOrder(t, db, "JS20260102011", hisID, 40, constants.SettlementReversed, past)
	rec, err := svc.Daily(ctx, hisID, past)
	if err != nil {
		t.Fatalf("Daily(past) error = %v", err)
	}
	if rec.ReconcileDate != past || rec.TotalCount != 2 || rec.TotalAmount != 160 ||
		rec.ReversedCount != 1 || rec.NetAmount != 120 {
		t.Fatalf("historical rec = %+v", rec)
	}

	// 非法日期格式
	if _, err := svc.Daily(ctx, hisID, "2026/01/02"); err == nil {
		t.Fatal("expected bad request for invalid date")
	}
}
