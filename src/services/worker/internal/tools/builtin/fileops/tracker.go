package fileops

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// LineRange represents a contiguous range of lines [Start, End] (1-based, inclusive).
type LineRange struct {
	Start int
	End   int
}

// FileReadState tracks the last-read state of a file for dedup purposes.
type FileReadState struct {
	Mtime  int64       // unix nanoseconds of file mtime at read time
	Ranges []LineRange // merged line ranges that have been read
}

// FileTracker records per-path read/write timestamps within a single run.
// Used by edit and write_file for safety checks (must read before edit).
type FileTracker struct {
	mu      sync.RWMutex
	records    map[string]map[string]fileRecord
	readStates map[string]map[string]*FileReadState // runID -> path -> state
}

type fileRecord struct {
	readTime  time.Time
	writeTime time.Time
}

func NewFileTracker() *FileTracker {
	return &FileTracker{
		records:    make(map[string]map[string]fileRecord),
		readStates: make(map[string]map[string]*FileReadState),
	}
}

func (t *FileTracker) RecordRead(path string) {
	t.RecordReadForRun("", path)
}

func (t *FileTracker) RecordReadForRun(runID string, path string) {
	runID = normalizeRunKey(runID)
	path = normalizePathKey(path)
	if path == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	runRecords := t.records[runID]
	if runRecords == nil {
		runRecords = make(map[string]fileRecord)
		t.records[runID] = runRecords
	}
	r := runRecords[path]
	r.readTime = time.Now()
	runRecords[path] = r
}

func (t *FileTracker) RecordWrite(path string) {
	t.RecordWriteForRun("", path)
}

func (t *FileTracker) RecordWriteForRun(runID string, path string) {
	runID = normalizeRunKey(runID)
	path = normalizePathKey(path)
	if path == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	runRecords := t.records[runID]
	if runRecords == nil {
		runRecords = make(map[string]fileRecord)
		t.records[runID] = runRecords
	}
	r := runRecords[path]
	r.writeTime = time.Now()
	runRecords[path] = r
}

func (t *FileTracker) LastReadTime(path string) time.Time {
	return t.LastReadTimeForRun("", path)
}

func (t *FileTracker) LastReadTimeForRun(runID string, path string) time.Time {
	runID = normalizeRunKey(runID)
	path = normalizePathKey(path)
	if path == "" {
		return time.Time{}
	}

	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.records[runID][path].readTime
}

func (t *FileTracker) HasBeenRead(path string) bool {
	return t.HasBeenReadForRun("", path)
}

func (t *FileTracker) HasBeenReadForRun(runID string, path string) bool {
	return !t.LastReadTimeForRun(runID, path).IsZero()
}

// RecordReadState records the mtime and line range for a file read.
// If the file was already read in this run, the range is merged.
func (t *FileTracker) RecordReadState(runID, path string, mtime int64, start, end int) {
	runID = normalizeRunKey(runID)
	path = normalizePathKey(path)
	if path == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	runStates := t.readStates[runID]
	if runStates == nil {
		runStates = make(map[string]*FileReadState)
		t.readStates[runID] = runStates
	}
	state := runStates[path]
	if state == nil || state.Mtime != mtime {
		// new file or mtime changed — reset state
		runStates[path] = &FileReadState{
			Mtime:  mtime,
			Ranges: []LineRange{{Start: start, End: end}},
		}
		return
	}
	state.Ranges = mergeRange(state.Ranges, LineRange{Start: start, End: end})
}

// CheckReadDedup returns true if the requested range is fully covered by
// previous reads and the file mtime has not changed.
func (t *FileTracker) CheckReadDedup(runID, path string, currentMtime int64, start, end int) bool {
	runID = normalizeRunKey(runID)
	path = normalizePathKey(path)
	if path == "" {
		return false
	}

	t.mu.RLock()
	defer t.mu.RUnlock()
	state := t.readStates[runID][path]
	if state == nil || state.Mtime != currentMtime {
		return false
	}
	return rangesCover(state.Ranges, start, end)
}

// InvalidateReadState clears the read state for a file (called after write/edit).
func (t *FileTracker) InvalidateReadState(runID, path string) {
	runID = normalizeRunKey(runID)
	path = normalizePathKey(path)
	if path == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if runStates := t.readStates[runID]; runStates != nil {
		delete(runStates, path)
	}
}

func (t *FileTracker) CleanupRun(runID string) {
	runID = normalizeRunKey(runID)
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.records, runID)
	delete(t.readStates, runID)
}

// mergeRange inserts a new range and merges overlapping/adjacent ranges.
func mergeRange(ranges []LineRange, nr LineRange) []LineRange {
	ranges = append(ranges, nr)
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].Start < ranges[j].Start })
	merged := make([]LineRange, 0, len(ranges))
	for _, r := range ranges {
		if len(merged) > 0 && r.Start <= merged[len(merged)-1].End+1 {
			if r.End > merged[len(merged)-1].End {
				merged[len(merged)-1].End = r.End
			}
		} else {
			merged = append(merged, r)
		}
	}
	return merged
}

// rangesCover checks if merged ranges fully cover [start, end].
func rangesCover(ranges []LineRange, start, end int) bool {
	for _, r := range ranges {
		if r.Start <= start && r.End >= end {
			return true
		}
	}
	return false
}

func TrackingKey(workDir string, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	base := strings.TrimSpace(workDir)
	if base != "" && !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	return normalizePathKey(path)
}

func normalizeRunKey(runID string) string {
	return strings.TrimSpace(runID)
}

func normalizePathKey(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}
