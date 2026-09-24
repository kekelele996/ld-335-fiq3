package handler

import (
	"log/slog"

	"github.com/blueship581/gbinsureapi/internal/middleware"
	"github.com/blueship581/gbinsureapi/internal/service"
	"github.com/blueship581/gbinsureapi/internal/util"
	"github.com/gin-gonic/gin"
)

// DailyReconciliationHandler 日终对账接口。
type DailyReconciliationHandler struct {
	svc *service.ReconciliationService
	log *slog.Logger
}

// NewDailyReconciliationHandler 构造日终对账接口。
func NewDailyReconciliationHandler(svc *service.ReconciliationService, log *slog.Logger) *DailyReconciliationHandler {
	return &DailyReconciliationHandler{svc: svc, log: log}
}

// Daily 日终对账。
// @Summary 日终对账
// @Description 按登录调用方汇总指定自然日（默认今天）：原结算笔数/金额保留，已冲正订单单独统计冲正笔数并从净额扣回；重复汇总仅覆盖本调用方记录
// @Tags reconciliations
// @Security ApiKeyAuth
// @Security BearerAuth
// @Param date query string false "对账自然日（YYYY-MM-DD），默认今天"
// @Success 200 {object} util.Response
// @Router /api/v1/reconciliations/daily [get]
func (h *DailyReconciliationHandler) Daily(c *gin.Context) {
	clientID, _ := c.Get(middleware.ClientIDKey)
	rec, err := h.svc.Daily(c.Request.Context(), clientID.(uint), c.Query("date"))
	if err != nil {
		c.Error(err)
		return
	}
	util.OK(c, rec)
}

// List 对账记录列表。
// @Summary 对账记录列表
// @Description 仅返回登录调用方自己的日终对账记录，可指定自然日回查
// @Tags reconciliations
// @Security ApiKeyAuth
// @Security BearerAuth
// @Param date query string false "自然日（YYYY-MM-DD）"
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} util.Response
// @Router /api/v1/reconciliations [get]
func (h *DailyReconciliationHandler) List(c *gin.Context) {
	clientID, _ := c.Get(middleware.ClientIDKey)
	page := parseQueryInt(c.Query("page"), 1)
	pageSize := parseQueryInt(c.Query("page_size"), 20)
	items, total, err := h.svc.List(c.Request.Context(), clientID.(uint), c.Query("date"), page, pageSize)
	if err != nil {
		c.Error(err)
		return
	}
	util.OK(c, util.PageData{List: items, Total: total, Page: page, Size: pageSize})
}
