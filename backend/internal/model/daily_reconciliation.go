package model

import "time"

// DailyReconciliation 日终对账（按调用方 + 自然日各存一条，重复汇总只覆盖调用方自己的记录）。
type DailyReconciliation struct {
	ID             uint      `gorm:"primaryKey" json:"id"`
	ClientID       uint      `gorm:"not null;uniqueIndex:uk_recon_client_date,priority:1" json:"client_id"`
	ReconcileDate  string    `gorm:"size:10;not null;uniqueIndex:uk_recon_client_date,priority:2" json:"reconcile_date"`
	TotalCount     int64     `json:"total_count"`
	TotalAmount    float64   `json:"total_amount"`
	SuccessCount   int64     `json:"success_count"`
	ReversedCount  int64     `json:"reversed_count"`
	ReversedAmount float64   `json:"reversed_amount"`
	NetAmount      float64   `json:"net_amount"`
	FailCount      int64     `json:"fail_count"`
	AbnormalOrders int64     `json:"abnormal_orders"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
