package router

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/blueship581/gbinsureapi/internal/config"
	"github.com/blueship581/gbinsureapi/internal/constants"
	"github.com/blueship581/gbinsureapi/internal/handler"
	"github.com/blueship581/gbinsureapi/internal/middleware"
	"github.com/blueship581/gbinsureapi/internal/model"
	"github.com/blueship581/gbinsureapi/internal/repository"
	"github.com/blueship581/gbinsureapi/internal/service"
	"github.com/blueship581/gbinsureapi/internal/util"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

type testClient struct {
	model.ApiClient
	apiKey string
}

func (c testClient) auth(t *testing.T, req *http.Request) {
	t.Helper()
	token, err := util.GenerateToken("test-secret", 24, c.ID, c.Name, c.Role, "service")
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-API-Key", c.apiKey)
	req.Header.Set("Authorization", "Bearer "+token)
}

func testSlog() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

func setupReconRouter(t *testing.T) (*gin.Engine, *gorm.DB, testClient, testClient) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(
		&model.ApiClient{}, &model.InsuredPerson{}, &model.UploadBatch{}, &model.FeeItem{},
		&model.Presettlement{}, &model.SettlementOrder{}, &model.DailyReconciliation{}, &model.AuditLog{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	const secret = "test-secret"
	hisKey, thirdKey := "ak_test_his", "ak_test_third"
	his := model.ApiClient{Name: "测试HIS", ClientType: constants.ClientTypeHIS, APIKeyHash: util.HashAPIKey(hisKey, secret), Role: "settlement", Status: constants.ClientActive, RateLimitQPS: 100}
	third := model.ApiClient{Name: "测试药房", ClientType: constants.ClientTypeThirdParty, APIKeyHash: util.HashAPIKey(thirdKey, secret), Role: "query", Status: constants.ClientActive, RateLimitQPS: 100}
	if err := db.Create(&his).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&third).Error; err != nil {
		t.Fatal(err)
	}

	orderRepo := repository.NewSettlementOrderRepository(db)
	recRepo := repository.NewDailyReconciliationRepository(db)
	clientSvc := service.NewApiClientService(repository.NewApiClientRepository(db), secret, secret, 24, testSlog())
	reconSvc := service.NewReconciliationService(orderRepo, recRepo, testSlog())
	settlementSvc := service.NewSettlementService(nil, orderRepo, nil, nil, nil, nil, testSlog())

	h := Handlers{
		Recon:      handler.NewDailyReconciliationHandler(reconSvc, testSlog()),
		Settlement: handler.NewSettlementOrderHandler(settlementSvc, testSlog()),
	}
	r := New(config.Config{JWTSecret: secret}, testSlog(), h, clientSvc,
		repository.NewAuditLogRepository(db), middleware.NewRateLimiter())
	return r, db, testClient{ApiClient: his, apiKey: hisKey}, testClient{ApiClient: third, apiKey: thirdKey}
}

func settledAt(t *testing.T, date string) *time.Time {
	t.Helper()
	d, err := time.ParseInLocation("2006-01-02", date, time.FixedZone("CST", 8*3600))
	if err != nil {
		t.Fatal(err)
	}
	at := d.Add(10 * time.Hour)
	return &at
}

func doDaily(t *testing.T, r http.Handler, c testClient, date string) model.DailyReconciliation {
	t.Helper()
	url := "/api/v1/reconciliations/daily"
	if date != "" {
		url += "?date=" + date
	}
	var resp struct {
		Data model.DailyReconciliation `json:"data"`
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	c.auth(t, req)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("daily status=%d body=%s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Data
}

func doList(t *testing.T, r http.Handler, c testClient, query string) util.PageData {
	t.Helper()
	var page util.PageData
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reconciliations"+query, nil)
	c.auth(t, req)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data util.PageData `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	page = resp.Data
	return page
}

func doSettlements(t *testing.T, r http.Handler, c testClient, query string) int64 {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/settlements"+query, nil)
	c.auth(t, req)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("settlements status=%d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data util.PageData `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.Data.Total
}

func TestDailyReconciliation_PerClientIsolation(t *testing.T) {
	r, db, his, third := setupReconRouter(t)
	date := util.TodayDate()
	at10 := settledAt(t, date)
	at11 := at10.Add(time.Hour)
	orders := []model.SettlementOrder{
		{SettlementNo: "T001", ClientID: his.ID, Status: constants.SettlementSettled, TotalAmount: 100, SettledAt: at10},
		{SettlementNo: "T002", ClientID: his.ID, Status: constants.SettlementReversed, TotalAmount: 30, SettledAt: at10, ReversedAt: &at11},
		{SettlementNo: "T003", ClientID: third.ID, Status: constants.SettlementSettled, TotalAmount: 200, SettledAt: at10},
	}
	if err := db.Create(&orders).Error; err != nil {
		t.Fatal(err)
	}

	// HIS 先日终：原结算 2 笔/130 保留，冲正 1 笔/30 单列，净额 100
	hisRec := doDaily(t, r, his, "")
	if hisRec.TotalCount != 2 || hisRec.TotalAmount != 130 ||
		hisRec.ReversedCount != 1 || hisRec.ReversedAmount != 30 || hisRec.NetAmount != 100 {
		t.Fatalf("HIS 汇总异常: %+v", hisRec)
	}
	// 药房后日终，不得冲掉 HIS 的记录
	thirdRec := doDaily(t, r, third, "")
	if thirdRec.TotalCount != 1 || thirdRec.TotalAmount != 200 || thirdRec.NetAmount != 200 {
		t.Fatalf("药房汇总异常: %+v", thirdRec)
	}
	hisRec2 := doDaily(t, r, his, date)
	if hisRec2.ID != hisRec.ID || hisRec2.TotalCount != 2 || hisRec2.NetAmount != 100 {
		t.Fatalf("HIS 记录被药房覆盖: %+v", hisRec2)
	}

	// 记录列表只看得到自己
	hisList := doList(t, r, his, "")
	if hisList.Total != 1 || len(hisList.List.([]any)) != 1 {
		t.Fatalf("HIS 列表应只有自己的 1 条: %+v", hisList)
	}
	thirdList := doList(t, r, third, "?date="+date)
	if thirdList.Total != 1 {
		t.Fatalf("药房列表应只有自己的记录: %+v", thirdList)
	}
	row := thirdList.List.([]any)[0].(map[string]any)
	if row["client_id"].(float64) != float64(third.ID) {
		t.Fatalf("药房看到了别人的记录: %v", row["client_id"])
	}

	// 结算单列表同样按登录调用方隔离
	if total := doSettlements(t, r, his, ""); total != 2 {
		t.Fatalf("HIS 结算单应为 2 笔, got %d", total)
	}
	// 即使传入 client_id 参数也不能越权查看药房
	if total := doSettlements(t, r, his, "?client_id="+strconv.Itoa(int(third.ID))); total != 2 {
		t.Fatalf("client_id 参数必须被忽略, got total=%d", total)
	}
}

func TestDailyReconciliation_DateBackfill(t *testing.T) {
	r, db, his, _ := setupReconRouter(t)
	past := "2026-01-02"
	if err := db.Create(&[]model.SettlementOrder{
		{SettlementNo: "H001", ClientID: his.ID, Status: constants.SettlementSettled, TotalAmount: 120, SettledAt: settledAt(t, past)},
	}).Error; err != nil {
		t.Fatal(err)
	}

	// 指定历史日期回查并生成
	rec := doDaily(t, r, his, past)
	if rec.ReconcileDate != past || rec.TotalCount != 1 || rec.TotalAmount != 120 || rec.NetAmount != 120 {
		t.Fatalf("历史日终异常: %+v", rec)
	}
	// 列表按日期过滤
	if list := doList(t, r, his, "?date="+past); list.Total != 1 {
		t.Fatalf("按日期回查应 1 条, got %d", list.Total)
	}
	// 非法日期格式 400
	req := httptest.NewRequest(http.MethodGet, "/api/v1/reconciliations/daily?date=20260102", nil)
	his.auth(t, req)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法日期应 400, got %d body=%s", w.Code, w.Body.String())
	}
}
