# ccusage-go

Minimal Claude Code usage tracker. Parses local JSONL logs and outputs a daily cost table.

## Install

```bash
go install github.com/abatilo/ccusage-go@latest
ccusage-go
```

## Usage

```bash
# Run without installing
go run github.com/abatilo/ccusage-go@latest
```

## Example Output

```
Date                        Input            Output        CacheWrite         CacheRead            Cost
-------------------------------------------------------------------------------------------------------
2025-12-22                448,656            36,456        16,633,211       202,208,270         $120.11
2025-12-23                141,960             2,324         4,656,420        64,524,810          $50.87
-------------------------------------------------------------------------------------------------------
Total                   3,869,001           405,744       158,286,494     2,002,534,397       $1,377.40
```

## Flags

- `-v` - verbose timing output
- `--no-cache` - skip cache, reparse all files
- `--clear-cache` - delete cache and rebuild

## Credits

Inspired by [ryoppippi/ccusage](https://github.com/ryoppippi/ccusage)
