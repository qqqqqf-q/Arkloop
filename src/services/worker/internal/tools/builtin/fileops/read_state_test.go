package fileops

import "testing"

func TestMergeRange(t *testing.T) {
	tests := []struct {
		name     string
		initial  []LineRange
		add      LineRange
		expected []LineRange
	}{
		{
			name:     "empty",
			initial:  nil,
			add:      LineRange{1, 100},
			expected: []LineRange{{1, 100}},
		},
		{
			name:     "non-overlapping",
			initial:  []LineRange{{1, 50}},
			add:      LineRange{100, 200},
			expected: []LineRange{{1, 50}, {100, 200}},
		},
		{
			name:     "overlapping",
			initial:  []LineRange{{1, 100}},
			add:      LineRange{50, 150},
			expected: []LineRange{{1, 150}},
		},
		{
			name:     "adjacent",
			initial:  []LineRange{{1, 100}},
			add:      LineRange{101, 200},
			expected: []LineRange{{1, 200}},
		},
		{
			name:     "subset",
			initial:  []LineRange{{1, 200}},
			add:      LineRange{50, 100},
			expected: []LineRange{{1, 200}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := mergeRange(append([]LineRange(nil), tc.initial...), tc.add)
			if len(result) != len(tc.expected) {
				t.Fatalf("got %v, want %v", result, tc.expected)
			}
			for i := range result {
				if result[i] != tc.expected[i] {
					t.Fatalf("got %v, want %v", result, tc.expected)
				}
			}
		})
	}
}

func TestRangesCover(t *testing.T) {
	ranges := []LineRange{{1, 100}, {200, 300}}
	if !rangesCover(ranges, 1, 100) {
		t.Fatal("should cover 1-100")
	}
	if !rangesCover(ranges, 50, 80) {
		t.Fatal("should cover 50-80")
	}
	if rangesCover(ranges, 1, 150) {
		t.Fatal("should not cover 1-150")
	}
	if rangesCover(ranges, 101, 199) {
		t.Fatal("should not cover gap")
	}
}

func TestReadDedupFullCycle(t *testing.T) {
	tracker := NewFileTracker()
	runID := "run-1"
	path := "/workspace/src/main.go"

	// first read: no dedup
	if tracker.CheckReadDedup(runID, path, 1000, 1, 100) {
		t.Fatal("should not dedup on first read")
	}

	// record read
	tracker.RecordReadState(runID, path, 1000, 1, 100)

	// same range, same mtime: dedup
	if !tracker.CheckReadDedup(runID, path, 1000, 1, 100) {
		t.Fatal("should dedup same range")
	}

	// subset: dedup
	if !tracker.CheckReadDedup(runID, path, 1000, 10, 50) {
		t.Fatal("should dedup subset")
	}

	// wider range: no dedup
	if tracker.CheckReadDedup(runID, path, 1000, 1, 200) {
		t.Fatal("should not dedup wider range")
	}

	// different mtime: no dedup
	if tracker.CheckReadDedup(runID, path, 2000, 1, 100) {
		t.Fatal("should not dedup changed mtime")
	}

	// invalidate after write
	tracker.InvalidateReadState(runID, path)
	if tracker.CheckReadDedup(runID, path, 1000, 1, 100) {
		t.Fatal("should not dedup after invalidation")
	}
}

func TestReadDedupMtimeChange(t *testing.T) {
	tracker := NewFileTracker()
	runID := "run-1"
	path := "/workspace/file.txt"

	tracker.RecordReadState(runID, path, 1000, 1, 50)

	// record with new mtime resets ranges
	tracker.RecordReadState(runID, path, 2000, 100, 150)

	// old range should not be covered anymore
	if tracker.CheckReadDedup(runID, path, 2000, 1, 50) {
		t.Fatal("old range should be reset after mtime change")
	}
	if !tracker.CheckReadDedup(runID, path, 2000, 100, 150) {
		t.Fatal("new range should be covered")
	}
}
