package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
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

// fullWalkJSONL does a full filesystem walk, collecting both JSONL files and directory mtimes.
func fullWalkJSONL(dir string) (files []string, dirs map[string]int64) {
	dirs = make(map[string]int64)
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			info, infoErr := d.Info()
			if infoErr == nil {
				dirs[path] = info.ModTime().UnixNano()
			}
			return nil
		}
		if strings.HasSuffix(path, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	return
}

// walkSubtree walks a single directory subtree and collects JSONL files and dir mtimes.
func walkSubtree(root string) (files []string, dirs map[string]int64) {
	dirs = make(map[string]int64)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			info, infoErr := d.Info()
			if infoErr == nil {
				dirs[path] = info.ModTime().UnixNano()
			}
			return nil
		}
		if strings.HasSuffix(path, ".jsonl") {
			files = append(files, path)
		}
		return nil
	})
	return
}

const fullWalkInterval = 5 * time.Minute

// findJSONLFiles discovers JSONL files using the directory manifest when available.
// On cold start or when the safety interval has elapsed, it does a full walk.
// On warm start, it stats known directories and only walks changed subtrees.
func findJSONLFiles(dir string, cache *CacheFile) ([]string, discoveryStats) {
	var dStats discoveryStats

	// Cold start or safety net: full walk
	needsFullWalk := cache == nil ||
		len(cache.Dirs) == 0 ||
		time.Since(cache.LastFullWalk) > fullWalkInterval

	if needsFullWalk {
		dStats.fullWalk = true
		files, dirs := fullWalkJSONL(dir)
		if cache != nil {
			cache.Dirs = dirs
			cache.LastFullWalk = time.Now()
		}
		dStats.dirsChecked = len(dirs)
		return files, dStats
	}

	// Warm start: stat known directories, walk only changed subtrees
	dStats.dirsChecked = len(cache.Dirs)

	// Separate dirs into changed vs unchanged
	changedRoots := make(map[string]bool)
	unchangedDirs := make(map[string]bool)

	for dirPath, cachedMtime := range cache.Dirs {
		info, err := os.Stat(dirPath)
		if err != nil {
			changedRoots[dirPath] = true
			dStats.dirsChanged++
			continue
		}
		currentMtime := info.ModTime().UnixNano()
		if currentMtime != cachedMtime {
			changedRoots[dirPath] = true
			dStats.dirsChanged++
		} else {
			unchangedDirs[dirPath] = true
		}
	}

	// Collect files from unchanged directories using the cache's known files
	fileSet := make(map[string]bool)
	for filePath := range cache.Files {
		parentDir := filepath.Dir(filePath)
		if unchangedDirs[parentDir] {
			fileSet[filePath] = true
			dStats.filesFromCache++
		}
	}

	// Walk changed subtrees
	roots := minimalRoots(changedRoots)
	for _, root := range roots {
		dStats.subtreesWalked++
		subtreeFiles, subtreeDirs := walkSubtree(root)
		for _, f := range subtreeFiles {
			fileSet[f] = true
		}
		for d, mtime := range subtreeDirs {
			cache.Dirs[d] = mtime
			delete(unchangedDirs, d)
		}
	}

	// Clean up deleted directories from manifest
	for dirPath := range cache.Dirs {
		if _, err := os.Stat(dirPath); err != nil {
			delete(cache.Dirs, dirPath)
		}
	}

	files := make([]string, 0, len(fileSet))
	for f := range fileSet {
		files = append(files, f)
	}
	sort.Strings(files)

	return files, dStats
}

// minimalRoots filters a set of directory paths to only the top-level roots,
// removing any path that is a descendant of another path in the set.
func minimalRoots(dirs map[string]bool) []string {
	paths := make([]string, 0, len(dirs))
	for d := range dirs {
		paths = append(paths, d)
	}
	sort.Strings(paths)

	var roots []string
	for _, p := range paths {
		isChild := false
		for _, root := range roots {
			if strings.HasPrefix(p, root+string(filepath.Separator)) {
				isChild = true
				break
			}
		}
		if !isChild {
			roots = append(roots, p)
		}
	}
	return roots
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
const CacheVersion = 2

var cacheMagic = [4]byte{'C', 'C', 'U', 'G'}

type FileCacheEntry struct {
	ModTime int64
	Size    int64
	Entries []EntryData
}

type CacheFile struct {
	Version      int
	Timezone     string
	Files        map[string]*FileCacheEntry
	Dirs         map[string]int64
	LastFullWalk time.Time
}

// Encoded types for string-interned binary cache
type EncodedCache struct {
	StringTable  []string
	Files        map[string]*EncodedFileCacheEntry
	Dirs         map[string]int64
	LastFullWalk time.Time
}

type EncodedFileCacheEntry struct {
	ModTime int64
	Size    int64
	Entries []EncodedEntry
}

type EncodedEntry struct {
	Key                 string
	DateIdx             int
	ModelIdx            int
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int
}

type discoveryStats struct {
	dirsChecked    int
	dirsChanged    int
	subtreesWalked int
	filesFromCache int
	fullWalk       bool
}

func getCacheDir() string {
	if dir := os.Getenv("XDG_CACHE_HOME"); dir != "" {
		return filepath.Join(dir, "ccusage")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".cache", "ccusage")
}

func getCachePath() string {
	return filepath.Join(getCacheDir(), "cache.bin")
}

func getLegacyCachePath() string {
	return filepath.Join(getCacheDir(), "cache.json")
}

func getLocalTimezone() string {
	zone, _ := time.Now().Zone()
	return zone
}

// loadLegacyJSONCache tries to read the old JSON cache format and migrate it
func loadLegacyJSONCache() *CacheFile {
	data, err := os.ReadFile(getLegacyCachePath())
	if err != nil {
		return nil
	}
	type jsonFileCacheEntry struct {
		ModTime int64        `json:"mtime"`
		Size    int64        `json:"size"`
		Entries []*EntryData `json:"entries"`
	}
	type jsonCacheFile struct {
		Version      int                            `json:"version"`
		Timezone     string                         `json:"timezone"`
		Files        map[string]*jsonFileCacheEntry  `json:"files"`
		Dirs         map[string]int64               `json:"dirs,omitempty"`
		LastFullWalk time.Time                      `json:"last_full_walk,omitempty"`
	}
	var legacy jsonCacheFile
	if json.Unmarshal(data, &legacy) != nil {
		return nil
	}
	cache := &CacheFile{
		Version:      legacy.Version,
		Timezone:     legacy.Timezone,
		Files:        make(map[string]*FileCacheEntry, len(legacy.Files)),
		Dirs:         legacy.Dirs,
		LastFullWalk: legacy.LastFullWalk,
	}
	for path, fe := range legacy.Files {
		entries := make([]EntryData, len(fe.Entries))
		for i, e := range fe.Entries {
			entries[i] = *e
		}
		cache.Files[path] = &FileCacheEntry{
			ModTime: fe.ModTime,
			Size:    fe.Size,
			Entries: entries,
		}
	}
	return cache
}

func loadCache() *CacheFile {
	data, err := os.ReadFile(getCachePath())
	if err != nil {
		return loadLegacyJSONCache()
	}

	// Verify binary header: magic + version
	if len(data) < 8 {
		return loadLegacyJSONCache()
	}
	if data[0] != cacheMagic[0] || data[1] != cacheMagic[1] || data[2] != cacheMagic[2] || data[3] != cacheMagic[3] {
		return loadLegacyJSONCache()
	}
	version := binary.LittleEndian.Uint32(data[4:8])
	if version != CacheVersion {
		return nil
	}

	// Decode gob payload
	var encoded EncodedCache
	if err := gob.NewDecoder(bytes.NewReader(data[8:])).Decode(&encoded); err != nil {
		return nil
	}

	// De-intern strings
	cache := &CacheFile{
		Version:      int(version),
		Timezone:     "",
		Files:        make(map[string]*FileCacheEntry, len(encoded.Files)),
		Dirs:         encoded.Dirs,
		LastFullWalk: encoded.LastFullWalk,
	}
	if len(encoded.StringTable) > 0 {
		cache.Timezone = encoded.StringTable[0]
	}

	for path, fe := range encoded.Files {
		entries := make([]EntryData, len(fe.Entries))
		for i, ee := range fe.Entries {
			dateStr := ""
			if ee.DateIdx >= 0 && ee.DateIdx < len(encoded.StringTable) {
				dateStr = encoded.StringTable[ee.DateIdx]
			}
			modelStr := ""
			if ee.ModelIdx >= 0 && ee.ModelIdx < len(encoded.StringTable) {
				modelStr = encoded.StringTable[ee.ModelIdx]
			}
			entries[i] = EntryData{
				Key:                 ee.Key,
				Date:                dateStr,
				Model:               modelStr,
				InputTokens:         ee.InputTokens,
				OutputTokens:        ee.OutputTokens,
				CacheCreationTokens: ee.CacheCreationTokens,
				CacheReadTokens:     ee.CacheReadTokens,
			}
		}
		cache.Files[path] = &FileCacheEntry{
			ModTime: fe.ModTime,
			Size:    fe.Size,
			Entries: entries,
		}
	}
	return cache
}

func saveCache(cache *CacheFile) error {
	dir := getCacheDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Build string table for interning
	stringIndex := make(map[string]int)
	var stringTable []string
	intern := func(s string) int {
		if idx, ok := stringIndex[s]; ok {
			return idx
		}
		idx := len(stringTable)
		stringTable = append(stringTable, s)
		stringIndex[s] = idx
		return idx
	}

	// Timezone is always index 0
	intern(cache.Timezone)

	// Build encoded structure
	encoded := EncodedCache{
		Files:        make(map[string]*EncodedFileCacheEntry, len(cache.Files)),
		Dirs:         cache.Dirs,
		LastFullWalk: cache.LastFullWalk,
	}
	for path, fe := range cache.Files {
		entries := make([]EncodedEntry, len(fe.Entries))
		for i, e := range fe.Entries {
			entries[i] = EncodedEntry{
				Key:                 e.Key,
				DateIdx:             intern(e.Date),
				ModelIdx:            intern(e.Model),
				InputTokens:         e.InputTokens,
				OutputTokens:        e.OutputTokens,
				CacheCreationTokens: e.CacheCreationTokens,
				CacheReadTokens:     e.CacheReadTokens,
			}
		}
		encoded.Files[path] = &EncodedFileCacheEntry{
			ModTime: fe.ModTime,
			Size:    fe.Size,
			Entries: entries,
		}
	}
	encoded.StringTable = stringTable

	// Encode: magic + version + gob payload
	var buf bytes.Buffer
	buf.Write(cacheMagic[:])
	var versionBytes [4]byte
	binary.LittleEndian.PutUint32(versionBytes[:], uint32(CacheVersion))
	buf.Write(versionBytes[:])

	if err := gob.NewEncoder(&buf).Encode(encoded); err != nil {
		return err
	}

	tmpPath := getCachePath() + ".tmp"
	if err := os.WriteFile(tmpPath, buf.Bytes(), 0644); err != nil {
		return err
	}

	// Remove legacy JSON cache if it exists
	os.Remove(getLegacyCachePath())

	return os.Rename(tmpPath, getCachePath())
}

// processFileForCache parses a JSONL file and returns entries keyed by dedup key
func processFileForCache(path string) (map[string]EntryData, FileStats) {
	entries := make(map[string]EntryData)
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

		if _, exists := entries[key]; !exists {
			entries[key] = EntryData{
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
	conflicts  int
}

func entryTotalTokens(e *EntryData) int {
	return e.InputTokens + e.OutputTokens + e.CacheCreationTokens + e.CacheReadTokens
}

// processWithCacheLoaded processes files using a pre-loaded cache.
func processWithCacheLoaded(files []string, cache *CacheFile, cacheValid bool) (map[string]*EntryData, cacheStats, bool) {
	var stats cacheStats
	dirty := false

	if !cacheValid {
		dirty = true
	}

	// Pre-size allEntries from cache data
	expectedCount := 0
	for _, fc := range cache.Files {
		expectedCount += len(fc.Entries)
	}
	allEntries := make(map[string]*EntryData, expectedCount)

	existingFiles := make(map[string]bool)

	// Phase 1: Sequential scan — handle cache hits, collect misses
	type cacheMiss struct {
		path  string
		mtime int64
		size  int64
	}
	var misses []cacheMiss

	for _, path := range files {
		existingFiles[path] = true
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		mtime := fi.ModTime().UnixNano()
		size := fi.Size()

		cached, ok := cache.Files[path]
		if cacheValid && ok && cached.ModTime == mtime && cached.Size == size {
			stats.hits++
			for i := range cached.Entries {
				e := &cached.Entries[i]
				if existing, ok := allEntries[e.Key]; ok {
					if entryTotalTokens(e) > entryTotalTokens(existing) {
						allEntries[e.Key] = e
						stats.conflicts++
					}
				} else {
					allEntries[e.Key] = e
					stats.totalNew++
				}
			}
		} else {
			stats.misses++
			misses = append(misses, cacheMiss{path: path, mtime: mtime, size: size})
		}
	}

	if len(misses) > 0 {
		dirty = true
	}

	// Phase 2: Concurrent parsing of cache misses
	type fileResult struct {
		path    string
		mtime   int64
		size    int64
		entries []EntryData
		stats   FileStats
	}
	results := make([]fileResult, len(misses))

	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())

	for i, miss := range misses {
		wg.Add(1)
		go func(i int, miss cacheMiss) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			entries, fStats := processFileForCache(miss.path)
			entrySlice := make([]EntryData, 0, len(entries))
			for _, e := range entries {
				entrySlice = append(entrySlice, e)
			}
			results[i] = fileResult{
				path:    miss.path,
				mtime:   miss.mtime,
				size:    miss.size,
				entries: entrySlice,
				stats:   fStats,
			}
		}(i, miss)
	}
	wg.Wait()

	// Phase 3: Sequential merge — dedup entries, update cache
	for ri := range results {
		r := &results[ri]
		stats.totalLines += r.stats.LinesRead
		for i := range r.entries {
			e := &r.entries[i]
			if existing, ok := allEntries[e.Key]; ok {
				if entryTotalTokens(e) > entryTotalTokens(existing) {
					allEntries[e.Key] = e
					stats.conflicts++
				}
			} else {
				allEntries[e.Key] = e
				stats.totalNew++
			}
		}
		cache.Files[r.path] = &FileCacheEntry{
			ModTime: r.mtime,
			Size:    r.size,
			Entries: r.entries,
		}
	}

	// Cleanup deleted files
	for path := range cache.Files {
		if !existingFiles[path] {
			dirty = true
			delete(cache.Files, path)
		}
	}

	return allEntries, stats, dirty
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

	// Load cache early so findJSONLFiles can use the directory manifest
	if *clearCache {
		os.Remove(getCachePath())
		os.Remove(getLegacyCachePath())
	}
	var cache *CacheFile
	if !*noCache && !*clearCache {
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

	// Phase 1: Find files (uses directory manifest on warm runs)
	start := time.Now()
	configDir := getConfigDir()
	files, dStats := findJSONLFiles(filepath.Join(configDir, "projects"), cache)
	findDuration := time.Since(start)

	// Phase 2: Process files (with caching)
	start = time.Now()
	entries, cStats, dirty := processWithCacheLoaded(files, cache, cacheValid)
	processDuration := time.Since(start)

	// Phase 3: Aggregate
	start = time.Now()
	dayUsage := aggregateUsage(entries)
	aggregateDuration := time.Since(start)

	// Phase 4: Print table
	start = time.Now()
	printTable(dayUsage, modelPricing)
	printDuration := time.Since(start)

	if *verbose {
		fmt.Fprintf(os.Stderr, "\n--- Timing ---\n")
		if dStats.fullWalk {
			fmt.Fprintf(os.Stderr, "Find files:     %v (%d files, full walk, %d dirs)\n",
				findDuration, len(files), dStats.dirsChecked)
		} else {
			fmt.Fprintf(os.Stderr, "Find files:     %v (%d files, %d dirs checked, %d changed, %d subtrees walked, %d from cache)\n",
				findDuration, len(files), dStats.dirsChecked, dStats.dirsChanged, dStats.subtreesWalked, dStats.filesFromCache)
		}
		fmt.Fprintf(os.Stderr, "Process files:  %v (cache: %d hits, %d misses, %d lines parsed, %d unique, %d conflicts)\n",
			processDuration, cStats.hits, cStats.misses, cStats.totalLines, cStats.totalNew, cStats.conflicts)
		fmt.Fprintf(os.Stderr, "Aggregate:      %v\n", aggregateDuration)
		fmt.Fprintf(os.Stderr, "Print table:    %v\n", printDuration)
		fmt.Fprintf(os.Stderr, "Total:          %v\n", time.Since(totalStart))
	}

	// Save cache after output so the user sees results immediately
	if dirty || dStats.fullWalk || dStats.dirsChanged > 0 {
		_ = saveCache(cache)
	}
}
