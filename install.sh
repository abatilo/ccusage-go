#!/usr/bin/env
go build -ldflags="-w -s" -o ccusage-go ./*.go
mv ccusage-go ~/.local/bin/ccusage
