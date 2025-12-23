# ccusage-go

Minimal Claude Code usage tracker. Reads JSONL logs, calculates costs, outputs daily table.

## Build & Run

```bash
go build .
./ccusage-go
```

## Data Sources

Reads `**/*.jsonl` from (in order):
1. `$CLAUDE_CONFIG_DIR/projects/` (if set)
2. `~/.config/claude/projects/` (if exists)
3. `~/.claude/projects/` (fallback)

## Pricing Config

Edit `pricing.toml` to set per-model rates (USD per million tokens):

```toml
[models.claude-sonnet-4-5-20250929]
input = 3.0
output = 15.0
cache_write = 3.75
cache_read = 0.30
```

Unknown models fall back to `[models.default]`.

## Architecture

Single file (`main.go`), stdlib only. Flow:
1. `loadPricing()` - parse TOML config
2. `findJSONLFiles()` - recursive glob
3. `processFile()` - stream JSONL, aggregate per-model per-day
4. `calculateCost()` - apply model pricing
5. `printTable()` - output with running total

## Development

```bash
go build .           # compile
go vet ./...         # static analysis
golangci-lint run    # lint
```
