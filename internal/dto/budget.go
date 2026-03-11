package dto

import "time"

type BudgetCounter struct {
	Key   string
	Limit int64
	Cost  int64
	TTL   time.Duration
}

type BudgetAdjustment struct {
	Key   string
	Delta int64
	TTL   time.Duration
}

type BudgetConsumeResult struct {
	Allowed    bool
	RetryAfter time.Duration
	FailedKey  string
}
