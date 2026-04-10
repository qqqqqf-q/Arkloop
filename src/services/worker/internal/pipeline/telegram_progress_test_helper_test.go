package pipeline

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"arkloop/services/shared/telegrambot"
)

func newTestTelegramProgressTracker(t *testing.T) (*TelegramProgressTracker, *fakeTelegramServer) {
	t.Helper()
	fake := &fakeTelegramServer{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(srv.Close)
	client := telegrambot.NewClient(srv.URL, nil)
	tracker := NewTelegramProgressTracker(client, "test-token", ChannelDeliveryTarget{
		Conversation: ChannelConversationRef{Target: "123"},
	}, nil)
	return tracker, fake
}

type fakeTelegramServer struct {
	mu             sync.Mutex
	sendCount      int
	editCount      int
	lastSendText   string
	lastEditText   string
	sendTexts      []string
	editTexts      []string
	sendMessageIDs []int64
	editMessageIDs []int64
	onEvent        func(string)
}

type fakeTelegramSnapshot struct {
	sendCount      int
	editCount      int
	lastSendText   string
	lastEditText   string
	sendTexts      []string
	editTexts      []string
	sendMessageIDs []int64
	editMessageIDs []int64
}

func (f *fakeTelegramServer) handler(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)

	if strings.HasSuffix(r.URL.Path, "/sendMessage") {
		f.sendCount++
		f.lastSendText, _ = body["text"].(string)
		f.sendTexts = append(f.sendTexts, f.lastSendText)
		messageID := int64(40 + f.sendCount)
		f.sendMessageIDs = append(f.sendMessageIDs, messageID)
		if f.onEvent != nil {
			f.onEvent(fmt.Sprintf("send:%s", f.lastSendText))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fmt.Sprintf(`{"ok":true,"result":{"message_id":%d}}`, messageID)))
		return
	}
	if strings.HasSuffix(r.URL.Path, "/editMessageText") {
		f.editCount++
		f.lastEditText, _ = body["text"].(string)
		f.editTexts = append(f.editTexts, f.lastEditText)
		if rawID, ok := body["message_id"].(float64); ok {
			messageID := int64(rawID)
			f.editMessageIDs = append(f.editMessageIDs, messageID)
			if f.onEvent != nil {
				f.onEvent(fmt.Sprintf("edit:%d:%s", messageID, f.lastEditText))
			}
		} else if f.onEvent != nil {
			f.onEvent(fmt.Sprintf("edit:%s", f.lastEditText))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
}

func (f *fakeTelegramServer) stats() (sends, edits int) {
	snapshot := f.snapshot()
	return snapshot.sendCount, snapshot.editCount
}

func (f *fakeTelegramServer) snapshot() fakeTelegramSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fakeTelegramSnapshot{
		sendCount:      f.sendCount,
		editCount:      f.editCount,
		lastSendText:   f.lastSendText,
		lastEditText:   f.lastEditText,
		sendTexts:      append([]string(nil), f.sendTexts...),
		editTexts:      append([]string(nil), f.editTexts...),
		sendMessageIDs: append([]int64(nil), f.sendMessageIDs...),
		editMessageIDs: append([]int64(nil), f.editMessageIDs...),
	}
}

func resetTelegramTrackerThrottle(tracker *TelegramProgressTracker) {
	tracker.mu.Lock()
	tracker.lastEdit = time.Time{}
	tracker.mu.Unlock()
}
