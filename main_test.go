package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProcessFileKeepsFinalStreamedUsage(t *testing.T) {
	lines := `{"timestamp":"2026-09-23T12:00:00Z","requestId":"req_1","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":5,"output_tokens":1,"cache_read_input_tokens":100}}}
{"timestamp":"2026-09-23T12:00:01Z","requestId":"req_1","message":{"id":"msg_1","model":"claude-opus-5-5","usage":{"input_tokens":5,"output_tokens":348,"cache_read_input_tokens":100}}}
`
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}

	entries, _ := processFileForCache(path)

	got := entries["msg_1:req_1"].OutputTokens
	if got != 348 {
		t.Fatalf("OutputTokens = %d, want 348", got)
	}
}
