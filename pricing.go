package main

// Model pricing (per million tokens, USD)
// See: https://platform.claude.com/docs/en/about-claude/pricing

var modelPricing = map[string]ModelPricing{
	// Default pricing (used for unknown models)
	"default": {Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheWrite1h: 6.0, CacheRead: 0.30},

	// Opus 4.6
	"claude-opus-4-6":      {Input: 5.0, Output: 25.0, CacheWrite: 6.25, CacheWrite1h: 10.0, CacheRead: 0.50},
	"claude-opus-4-6:fast": {Input: 30.0, Output: 150.0, CacheWrite: 37.50, CacheWrite1h: 60.0, CacheRead: 3.0},

	// Opus 4.5
	"claude-opus-4-5-20251101":      {Input: 5.0, Output: 25.0, CacheWrite: 6.25, CacheWrite1h: 10.0, CacheRead: 0.50},
	"claude-opus-4-5-20251101:fast": {Input: 30.0, Output: 150.0, CacheWrite: 37.50, CacheWrite1h: 60.0, CacheRead: 3.0},

	// Opus 4.1 (legacy pricing)
	"claude-opus-4-1-20250805": {Input: 15.0, Output: 75.0, CacheWrite: 18.75, CacheWrite1h: 30.0, CacheRead: 1.50},

	// Sonnet 4
	"claude-sonnet-4-20250514":      {Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheWrite1h: 6.0, CacheRead: 0.30},
	"claude-sonnet-4-20250514:fast": {Input: 18.0, Output: 90.0, CacheWrite: 22.50, CacheWrite1h: 36.0, CacheRead: 1.80},

	// Sonnet 4.5
	"claude-sonnet-4-5-20250514":      {Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheWrite1h: 6.0, CacheRead: 0.30},
	"claude-sonnet-4-5-20250514:fast": {Input: 18.0, Output: 90.0, CacheWrite: 22.50, CacheWrite1h: 36.0, CacheRead: 1.80},
	"claude-sonnet-4-5-20250929":      {Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheWrite1h: 6.0, CacheRead: 0.30},
	"claude-sonnet-4-5-20250929:fast": {Input: 18.0, Output: 90.0, CacheWrite: 22.50, CacheWrite1h: 36.0, CacheRead: 1.80},

	// Haiku 3.5
	"claude-haiku-3-5-20241022": {Input: 0.80, Output: 4.0, CacheWrite: 1.0, CacheWrite1h: 1.60, CacheRead: 0.08},

	// Haiku 4.5
	"claude-haiku-4-5-20251001":      {Input: 1.0, Output: 5.0, CacheWrite: 1.25, CacheWrite1h: 2.0, CacheRead: 0.10},
	"claude-haiku-4-5-20251001:fast": {Input: 6.0, Output: 30.0, CacheWrite: 7.50, CacheWrite1h: 12.0, CacheRead: 0.60},

	// Short-form model names
	"haiku":      {Input: 1.0, Output: 5.0, CacheWrite: 1.25, CacheWrite1h: 2.0, CacheRead: 0.10},
	"haiku:fast": {Input: 6.0, Output: 30.0, CacheWrite: 7.50, CacheWrite1h: 12.0, CacheRead: 0.60},
	"sonnet":      {Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheWrite1h: 6.0, CacheRead: 0.30},
	"sonnet:fast": {Input: 18.0, Output: 90.0, CacheWrite: 22.50, CacheWrite1h: 36.0, CacheRead: 1.80},
	"opus":      {Input: 5.0, Output: 25.0, CacheWrite: 6.25, CacheWrite1h: 10.0, CacheRead: 0.50},
	"opus:fast": {Input: 30.0, Output: 150.0, CacheWrite: 37.50, CacheWrite1h: 60.0, CacheRead: 3.0},
}
