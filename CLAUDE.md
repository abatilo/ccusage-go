# ccusage-go

Minimal Claude Code usage tracker. Reads JSONL logs, calculates costs, outputs daily table.

## Build & Run

```bash
go build .
./ccusage-go
./ccusage-go -v           # verbose timing output
./ccusage-go --no-cache   # skip cache, reparse all files
./ccusage-go --clear-cache # delete cache and rebuild
```

## Data Sources

Reads `**/*.jsonl` from (in order):
1. `$CLAUDE_CONFIG_DIR/projects/` (if set)
2. `~/.config/claude/projects/` (if exists)
3. `~/.claude/projects/` (fallback)

## Pricing

Model pricing is hardcoded in `pricing.go`. Edit that file to update rates.
Unknown models fall back to `"default"` pricing.

## Caching

Parsed entries are cached at `~/.cache/ccusage/cache.json` (respects `$XDG_CACHE_HOME`).
Cache is keyed by file modification time - only changed files are reparsed.

## Architecture

Stdlib only. Flow:
1. `findJSONLFiles()` - recursive glob
2. `processWithCache()` - check cache, parse changed files
3. `aggregateUsage()` - group by date and model
4. `calculateCost()` - apply model pricing from `pricing.go`
5. `printTable()` - output daily rows with total row at bottom

## Development

```bash
go build .           # compile
go vet ./...         # static analysis
golangci-lint run    # lint
```
