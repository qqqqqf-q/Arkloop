//go:build !windows

package pipeline

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCompactThreadCompactionLockDesktopUnixReleasesFlock(t *testing.T) {
	t.Setenv("ARKLOOP_RUNDIR", t.TempDir())

	threadID := uuid.New()
	_, releaseFirst, err := CompactThreadCompactionLock(context.Background(), nil, threadID)
	if err != nil {
		t.Fatalf("acquire first lock: %v", err)
	}

	blockedCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, releaseSecond, err := CompactThreadCompactionLock(blockedCtx, nil, threadID); err == nil {
		releaseSecond()
		releaseFirst()
		t.Fatal("second lock acquired while first lock was held")
	}

	releaseFirst()

	_, releaseSecond, err := CompactThreadCompactionLock(context.Background(), nil, threadID)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	releaseSecond()
}

func TestLockDesktopFileUnixUnlocksBeforeClose(t *testing.T) {
	lockPath := t.TempDir() + "/compact.lock"
	first, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatalf("open first lock file: %v", err)
	}
	defer first.Close()

	releaseFirst, err := lockDesktopFile(context.Background(), first)
	if err != nil {
		t.Fatalf("acquire first flock: %v", err)
	}

	second, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		releaseFirst()
		t.Fatalf("open second lock file: %v", err)
	}
	defer second.Close()

	blockedCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if releaseSecond, err := lockDesktopFile(blockedCtx, second); err == nil {
		releaseSecond()
		releaseFirst()
		t.Fatal("second flock acquired while first flock was held")
	}

	releaseFirst()

	releaseSecond, err := lockDesktopFile(context.Background(), second)
	if err != nil {
		t.Fatalf("acquire second flock after release: %v", err)
	}
	releaseSecond()
}
