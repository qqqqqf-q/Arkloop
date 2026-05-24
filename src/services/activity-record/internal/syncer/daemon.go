package syncer

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"arkloop/services/activity-record/internal/sources/clipboard"
	"arkloop/services/activity-record/internal/sources/keyboard"
	"arkloop/services/activity-record/internal/sources/mouse"
	"arkloop/services/activity-record/internal/sources/window"
	"arkloop/services/activity-record/internal/store"
)

type DaemonSource interface {
	Source
	Run(ctx context.Context, db *store.Store, events chan<- store.Event) error
}

type DaemonOptions struct {
	DataDir       string
	SyncSources   []string
	DaemonSources []string
	SyncInterval  time.Duration
	IdleThreshold time.Duration
}

func Daemon(ctx context.Context, opts DaemonOptions) error {
	if opts.DataDir == "" {
		return errDataDirRequired
	}
	if opts.SyncInterval <= 0 {
		opts.SyncInterval = 5 * time.Minute
	}
	db, err := store.Open(filepath.Join(opts.DataDir, "activity.db"))
	if err != nil {
		return err
	}
	defer db.Close()

	pidPath := filepath.Join(opts.DataDir, "activity-record.pid")
	if err := writePID(pidPath); err != nil {
		return err
	}
	defer os.Remove(pidPath)

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	events := make(chan store.Event, 256)
	var srcWg sync.WaitGroup
	var svcWg sync.WaitGroup

	for _, name := range opts.DaemonSources {
		src, err := buildDaemonSource(name, opts)
		if err != nil {
			log.Printf("daemon source %s: skip: %v", name, err)
			continue
		}
		srcWg.Add(1)
		go func(s DaemonSource) {
			defer srcWg.Done()
			if err := s.Run(ctx, db, events); err != nil && ctx.Err() == nil {
				log.Printf("daemon source %s: %v", s.Name(), err)
			}
		}(src)
	}

	svcWg.Add(1)
	go func() {
		defer svcWg.Done()
		collectEvents(ctx, db, events)
	}()

	if len(opts.SyncSources) > 0 {
		runSyncSources(ctx, db, opts.SyncSources)

		svcWg.Add(1)
		go func() {
			defer svcWg.Done()
			ticker := time.NewTicker(opts.SyncInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					runSyncSources(ctx, db, opts.SyncSources)
				}
			}
		}()
	}

	<-ctx.Done()
	srcWg.Wait()
	close(events)
	svcWg.Wait()
	return nil
}

func collectEvents(ctx context.Context, db *store.Store, events <-chan store.Event) {
	batch := make([]store.Event, 0, 64)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		changed, err := db.UpsertEvents(ctx, batch)
		if err != nil {
			log.Printf("flush events: %v", err)
		} else if changed > 0 {
			log.Printf("flushed %d events", changed)
		}
		batch = batch[:0]
	}

	for {
		select {
		case event, ok := <-events:
			if !ok {
				flush()
				return
			}
			batch = append(batch, event)
			if len(batch) >= 64 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func runSyncSources(ctx context.Context, db *store.Store, sourceNames []string) {
	var wg sync.WaitGroup
	for _, name := range sourceNames {
		if ctx.Err() != nil {
			return
		}
		src, err := buildSource(name)
		if err != nil {
			log.Printf("sync source %s: skip: %v", name, err)
			continue
		}
		wg.Add(1)
		go func(s Source) {
			defer wg.Done()
			count, err := s.Sync(ctx, db)
			if err != nil {
				log.Printf("sync source=%s error=%v", s.Name(), err)
				return
			}
			log.Printf("sync source=%s events=%d", s.Name(), count)
		}(src)
	}
	wg.Wait()
}

func buildDaemonSource(name string, opts DaemonOptions) (DaemonSource, error) {
	switch name {
	case "window":
		return window.New(opts.IdleThreshold), nil
	case "clipboard":
		return clipboard.New(true), nil
	case "keyboard":
		return keyboard.New(), nil
	case "mouse":
		return mouse.New(), nil
	default:
		return nil, errUnknownSource(name)
	}
}

func writePID(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}
