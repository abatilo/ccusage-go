package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	Key                 string `json:"key"`
	Date                string `json:"date"`
	Model               string `json:"model"`
	InputTokens         int    `json:"input_tokens"`
	OutputTokens        int    `json:"output_tokens"`
	CacheCreationTokens int    `json:"cache_creation_tokens"`
	CacheReadTokens     int    `json:"cache_read_tokens"`
}

type FileStats struct {
	LinesRead   int
	LinesParsed int
	EntriesNew  int
}

// Cache types
const CacheVersion = 1

type FileCacheEntry struct {
	ModTime int64        `json:"mtime"`
	Entries []*EntryData `json:"entries"`
}

type CacheFile struct {
	Version  int                        `json:"version"`
	Timezone string                     `json:"timezone"`
	Files    map[string]*FileCacheEntry `json:"files"`
}

func getCacheDir() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "ccusage")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "ccusage")
}

func getCachePath() string {
	return filepath.Join(getCacheDir(), "cache.json")
}

func getLocalTimezone() string {
	zone, _ := time.Now().Zone()
	return zone
}

func loadCache() *CacheFile {
	data, err := os.ReadFile(getCachePath())
	if err != nil {
		return nil
	}
	var cache CacheFile
	if json.Unmarshal(data, &cache) != nil {
		return nil
	}
	return &cache
}

func saveCache(cache *CacheFile) error {
	dir := getCacheDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}
	tmpPath := getCachePath() + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmpPath, getCachePath())
}

// processFileForCache parses a JSONL file and returns entries keyed by dedup key
func processFileForCache(path string) (map[string]*EntryData, FileStats) {
	entries := make(map[string]*EntryData)
	var stats FileStats

	f, err := os.Open(path)
	if err != nil {
		return entries, stats
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		stats.LinesRead++
		var entry LogEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		stats.LinesParsed++
		if entry.Timestamp == "" || (entry.Message.Usage.InputTokens == 0 && entry.Message.Usage.OutputTokens == 0) {
			continue
		}
		if entry.Message.ID == "" || entry.RequestID == "" {
			continue
		}

		key := entry.Message.ID + ":" + entry.RequestID
		t, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err != nil {
			continue
		}
		date := t.Local().Format("2006-01-02")
		model := entry.Message.Model
		if model == "" {
			model = "unknown"
		}

		if entries[key] == nil {
			entries[key] = &EntryData{
				Key:                 key,
				Date:                date,
				Model:               model,
				InputTokens:         entry.Message.Usage.InputTokens,
				OutputTokens:        entry.Message.Usage.OutputTokens,
				CacheCreationTokens: entry.Message.Usage.CacheCreationTokens,
				CacheReadTokens:     entry.Message.Usage.CacheReadTokens,
			}
			stats.EntriesNew++
		}
	}
	return entries, stats
}

type cacheStats struct {
	hits       int
	misses     int
	totalLines int
	totalNew   int
}

func processWithCache(files []string, noCache bool, clearCache bool) (map[string]*EntryData, cacheStats) {
	var stats cacheStats
	allEntries := make(map[string]*EntryData)

	if clearCache {
		os.Remove(getCachePath())
	}

	var cache *CacheFile
	if !noCache && !clearCache {
		cache = loadCache()
	}

	cacheValid := cache != nil &&
		cache.Version == CacheVersion &&
		cache.Timezone == getLocalTimezone()

	if !cacheValid {
		cache = &CacheFile{
			Version:  CacheVersion,
			Timezone: getLocalTimezone(),
			Files:    make(map[string]*FileCacheEntry),
		}
	}

	existingFiles := make(map[string]bool)

	for _, path := range files {
		existingFiles[path] = true
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		mtime := fi.ModTime().Unix()

		cached, ok := cache.Files[path]
		if cacheValid && ok && cached.ModTime == mtime {
			// Cache hit
			stats.hits++
			for _, e := range cached.Entries {
				if allEntries[e.Key] == nil {
					allEntries[e.Key] = e
					stats.totalNew++
				}
			}
		} else {
			// Cache miss
			stats.misses++
			entries, fileStats := processFileForCache(path)
			stats.totalLines += fileStats.LinesRead

			entrySlice := make([]*EntryData, 0, len(entries))
			for _, e := range entries {
				entrySlice = append(entrySlice, e)
				if allEntries[e.Key] == nil {
					allEntries[e.Key] = e
					stats.totalNew++
				}
			}

			cache.Files[path] = &FileCacheEntry{
				ModTime: mtime,
				Entries: entrySlice,
			}
		}
	}

	// Cleanup deleted files
	for path := range cache.Files {
		if !existingFiles[path] {
			delete(cache.Files, path)
		}
	}

	_ = saveCache(cache)

	return allEntries, stats
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

func formatNumber(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

func printTable(dayUsage map[string]*DayUsage, pricing map[string]ModelPricing) {
	var dates []string
	for d := range dayUsage {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	fmt.Printf("%-15s %17s %17s %17s %17s %15s\n",
		"Date", "Input", "Output", "CacheWrite", "CacheRead", "Cost")
	fmt.Println(strings.Repeat("-", 103))

	var totalInput, totalOutput, totalCacheWrite, totalCacheRead int
	var totalCost float64
	for _, date := range dates {
		day := dayUsage[date]
		input, output, cacheWrite, cacheRead := sumUsage(day)
		cost := calculateCost(day, pricing)
		totalInput += input
		totalOutput += output
		totalCacheWrite += cacheWrite
		totalCacheRead += cacheRead
		totalCost += cost
		fmt.Printf("%-15s %17s %17s %17s %17s %15s\n",
			date,
			formatNumber(input),
			formatNumber(output),
			formatNumber(cacheWrite),
			formatNumber(cacheRead),
			fmt.Sprintf("$%.2f", cost))
	}

	fmt.Println(strings.Repeat("-", 103))
	fmt.Printf("%-15s %17s %17s %17s %17s %15s\n",
		"Total",
		formatNumber(totalInput),
		formatNumber(totalOutput),
		formatNumber(totalCacheWrite),
		formatNumber(totalCacheRead),
		fmt.Sprintf("$%.2f", totalCost))
}

func main() {
	verbose := flag.Bool("v", false, "verbose timing output")
	noCache := flag.Bool("no-cache", false, "skip reading cache (still writes cache)")
	clearCache := flag.Bool("clear-cache", false, "delete cache and rebuild")
	flag.Parse()

	totalStart := time.Now()

	// Phase 1: Find files
	start := time.Now()
	configDir := getConfigDir()
	files := findJSONLFiles(filepath.Join(configDir, "projects"))
	findDuration := time.Since(start)

	// Phase 3: Process files (with caching)
	start = time.Now()
	entries, cStats := processWithCache(files, *noCache, *clearCache)
	processDuration := time.Since(start)

	// Phase 4: Aggregate
	start = time.Now()
	dayUsage := aggregateUsage(entries)
	aggregateDuration := time.Since(start)

	// Phase 5: Print table
	start = time.Now()
	printTable(dayUsage, modelPricing)
	printDuration := time.Since(start)

	if *verbose {
		fmt.Fprintf(os.Stderr, "\n--- Timing ---\n")
		fmt.Fprintf(os.Stderr, "Find files:     %v (%d files)\n", findDuration, len(files))
		fmt.Fprintf(os.Stderr, "Process files:  %v (cache: %d hits, %d misses, %d lines parsed, %d unique)\n",
			processDuration, cStats.hits, cStats.misses, cStats.totalLines, cStats.totalNew)
		fmt.Fprintf(os.Stderr, "Aggregate:      %v\n", aggregateDuration)
		fmt.Fprintf(os.Stderr, "Print table:    %v\n", printDuration)
		fmt.Fprintf(os.Stderr, "Total:          %v\n", time.Since(totalStart))
	}
}
