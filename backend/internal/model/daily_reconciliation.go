package model

import "time"

// DailyReconciliation 日终对账（按调用方 + 自然日分别保存，重复汇总仅覆盖本调用方记录）。
type DailyReconciliation struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	ClientID       uint      `gorm:"uniqueIndex:uk_recon_client_date,priority:1;not null" json:"client_id"`
	ReconcileDate  string    `gorm:"size:10;uniqueIndex:uk_recon_client_date,priority:2;not null" json:"reconcile_date"`
	TotalCount     int64     `json:"total_count"`
	TotalAmount    float64   `json:"total_amount"`
	SuccessCount   int64     `json:"success_count"`
	FailCount      int64     `json:"fail_count"`
	AbnormalOrders int64     `json:"abnormal_orders"`
	ReversedCount  int64     `json:"reversed_count"`
	ReversedAmount float64   `json:"reversed_amount"`
	NetAmount      float64   `json:"net_amount"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
