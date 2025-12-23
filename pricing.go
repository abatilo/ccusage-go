package main

// Model pricing (per million tokens, USD)
// See: https://claude.com/pricing

var modelPricing = map[string]ModelPricing{
	// Default pricing (used for unknown models)
	"default": {Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheRead: 0.30},

	// Opus 4.5
	"claude-opus-4-5-20251101": {Input: 5.0, Output: 25.0, CacheWrite: 6.25, CacheRead: 0.50},

	// Opus 4.1 (legacy pricing)
	"claude-opus-4-1-20250805": {Input: 15.0, Output: 75.0, CacheWrite: 18.75, CacheRead: 1.50},

	// Sonnet 4
	"claude-sonnet-4-20250514": {Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheRead: 0.30},

	// Sonnet 4.5
	"claude-sonnet-4-5-20250514": {Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheRead: 0.30},
	"claude-sonnet-4-5-20250929": {Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheRead: 0.30},

	// Haiku 3.5
	"claude-haiku-3-5-20241022": {Input: 0.80, Output: 4.0, CacheWrite: 1.0, CacheRead: 0.08},

	// Haiku 4.5
	"claude-haiku-4-5-20251001": {Input: 1.0, Output: 5.0, CacheWrite: 1.25, CacheRead: 0.10},

	// Short-form model names
	"haiku":  {Input: 1.0, Output: 5.0, CacheWrite: 1.25, CacheRead: 0.10},
	"sonnet": {Input: 3.0, Output: 15.0, CacheWrite: 3.75, CacheRead: 0.30},
}
