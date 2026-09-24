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
// @Description 返回当前调用方指定自然日（默认今天）的原结算笔数/金额、冲正笔数/金额与净额；重复汇总仅覆盖该调用方自己的记录
// @Tags reconciliations
// @Security ApiKeyAuth
// @Security BearerAuth
// @Param date query string false "对账日期 YYYY-MM-DD，默认今天（可回查历史日期并重算）"
// @Success 200 {object} util.Response
// @Router /api/v1/reconciliations/daily [get]
func (h *DailyReconciliationHandler) Daily(c *gin.Context) {
	rec, err := h.svc.Daily(c.Request.Context(), currentClientID(c), c.Query("date"))
	if err != nil {
		c.Error(err)
		return
	}
	util.OK(c, rec)
}

// List 对账记录列表。
// @Summary 对账记录列表
// @Description 仅返回当前登录调用方自己的对账记录，可通过 date 指定自然日回查
// @Tags reconciliations
// @Security ApiKeyAuth
// @Security BearerAuth
// @Param date query string false "自然日 YYYY-MM-DD，不传则返回全部日期"
// @Param page query int false "页码"
// @Param page_size query int false "每页数量"
// @Success 200 {object} util.Response
// @Router /api/v1/reconciliations [get]
func (h *DailyReconciliationHandler) List(c *gin.Context) {
	page := parseQueryInt(c.Query("page"), 1)
	pageSize := parseQueryInt(c.Query("page_size"), 20)
	items, total, err := h.svc.List(c.Request.Context(), currentClientID(c), c.Query("date"), page, pageSize)
	if err != nil {
		c.Error(err)
		return
	}
	util.OK(c, util.PageData{List: items, Total: total, Page: page, Size: pageSize})
}

// currentClientID 从双认证中间件注入的上下文取登录调用方 ID（业务接口必经 ApiKey + JWT 中间件）。
func currentClientID(c *gin.Context) uint {
	if v, ok := c.Get(middleware.ClientIDKey); ok {
		if id, ok := v.(uint); ok {
			return id
		}
	}
	return 0
}
