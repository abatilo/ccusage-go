package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type LogEntry struct {
	Timestamp string `json:"timestamp"`
	RequestID string `json:"requestId"`
	Message   struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			InputTokens         int `json:"input_tokens"`
			OutputTokens        int `json:"output_tokens"`
			CacheCreationTokens int `json:"cache_creation_input_tokens"`
			CacheReadTokens     int `json:"cache_read_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

type ModelPricing struct {
	Input      float64
	Output     float64
	CacheWrite float64
	CacheRead  float64
}

type Usage struct {
	Input      int
	Output     int
	CacheWrite int
	CacheRead  int
}

type DayUsage struct {
	Models map[string]*Usage
}

func getConfigDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, _ := os.UserHomeDir()
	primary := filepath.Join(home, ".config", "claude")
	if _, err := os.Stat(filepath.Join(primary, "projects")); err == nil {
		return primary
	}
	return filepath.Join(home, ".claude")
}

func findJSONLFiles(dir string) []string {
	var files []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	return files
}

// EntryData stores parsed entry info for deduplication
type EntryData struct {
	Date                string
	Model               string
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
}

func processFile(path string, entries map[string]*EntryData) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		var entry LogEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if entry.Timestamp == "" || (entry.Message.Usage.InputTokens == 0 && entry.Message.Usage.OutputTokens == 0) {
			continue
		}

		// Build dedup key from message.id + requestId
		// Streaming creates multiple entries with cumulative output tokens - keep the last/max
		if entry.Message.ID == "" || entry.RequestID == "" {
			continue // Skip entries we can't deduplicate
		}
		key := entry.Message.ID + ":" + entry.RequestID

		// Parse timestamp and convert to local date (ccusage uses timezone conversion)
		t, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err != nil {
			continue
		}
		date := t.Local().Format("2006-01-02")
		model := entry.Message.Model
		if model == "" {
			model = "unknown"
		}

		// Keep first entry seen for each unique key (ccusage behavior)
		if entries[key] == nil {
			entries[key] = &EntryData{
				Date:                date,
				Model:               model,
				InputTokens:         entry.Message.Usage.InputTokens,
				OutputTokens:        entry.Message.Usage.OutputTokens,
				CacheCreationTokens: entry.Message.Usage.CacheCreationTokens,
				CacheReadTokens:     entry.Message.Usage.CacheReadTokens,
			}
		}
	}
}

func aggregateUsage(entries map[string]*EntryData) map[string]*DayUsage {
	dayUsage := make(map[string]*DayUsage)
	for _, e := range entries {
		if dayUsage[e.Date] == nil {
			dayUsage[e.Date] = &DayUsage{Models: make(map[string]*Usage)}
		}
		if dayUsage[e.Date].Models[e.Model] == nil {
			dayUsage[e.Date].Models[e.Model] = &Usage{}
		}
		dayUsage[e.Date].Models[e.Model].Input += e.InputTokens
		dayUsage[e.Date].Models[e.Model].Output += e.OutputTokens
		dayUsage[e.Date].Models[e.Model].CacheWrite += e.CacheCreationTokens
		dayUsage[e.Date].Models[e.Model].CacheRead += e.CacheReadTokens
	}
	return dayUsage
}

func loadPricing(path string) (map[string]ModelPricing, error) {
	pricing := make(map[string]ModelPricing)
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var currentModel string
	var current ModelPricing
	scanner := bufio.NewScanner(f)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[models.") && strings.HasSuffix(line, "]") {
			if currentModel != "" {
				pricing[currentModel] = current
			}
			currentModel = strings.TrimSuffix(strings.TrimPrefix(line, "[models."), "]")
			current = ModelPricing{}
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val, _ := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		switch key {
		case "input":
			current.Input = val
		case "output":
			current.Output = val
		case "cache_write":
			current.CacheWrite = val
		case "cache_read":
			current.CacheRead = val
		}
	}
	if currentModel != "" {
		pricing[currentModel] = current
	}
	return pricing, nil
}

func calculateCost(day *DayUsage, pricing map[string]ModelPricing) float64 {
	var total float64
	for model, usage := range day.Models {
		p := pricing[model]
		if p.Input == 0 && p.Output == 0 {
			p = pricing["default"]
		}
		total += (float64(usage.Input)*p.Input +
			float64(usage.Output)*p.Output +
			float64(usage.CacheWrite)*p.CacheWrite +
			float64(usage.CacheRead)*p.CacheRead) / 1_000_000
	}
	return total
}

func sumUsage(day *DayUsage) (input, output, cacheWrite, cacheRead int) {
	for _, u := range day.Models {
		input += u.Input
		output += u.Output
		cacheWrite += u.CacheWrite
		cacheRead += u.CacheRead
	}
	return
}

func printTable(dayUsage map[string]*DayUsage, pricing map[string]ModelPricing) {
	var dates []string
	for d := range dayUsage {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	fmt.Printf("%-12s %12s %12s %12s %12s %10s %10s\n",
		"Date", "Input", "Output", "CacheWrite", "CacheRead", "Cost", "Total")
	fmt.Println(strings.Repeat("-", 82))

	var runningTotal float64
	for _, date := range dates {
		day := dayUsage[date]
		input, output, cacheWrite, cacheRead := sumUsage(day)
		cost := calculateCost(day, pricing)
		runningTotal += cost
		fmt.Printf("%-12s %12d %12d %12d %12d %10.2f %10.2f\n",
			date, input, output, cacheWrite, cacheRead, cost, runningTotal)
	}
}

func main() {
	pricing, err := loadPricing("pricing.toml")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading pricing.toml: %v\n", err)
		os.Exit(1)
	}

	configDir := getConfigDir()
	files := findJSONLFiles(filepath.Join(configDir, "projects"))

	entries := make(map[string]*EntryData)
	for _, f := range files {
		processFile(f, entries)
	}

	dayUsage := aggregateUsage(entries)
	printTable(dayUsage, pricing)
}
