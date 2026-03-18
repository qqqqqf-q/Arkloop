package acp

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLocalProcessHostLifecycle(t *testing.T) {
	host := NewLocalProcessHost()
	ctx := context.Background()

	startResp, err := host.Start(ctx, StartRequest{
		SessionID: "run-1",
		Command:   []string{"sh", "-lc", "printf 'hello'; sleep 0.1"},
	})
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	readResp, err := host.Read(ctx, ReadRequest{SessionID: "run-1", ProcessID: startResp.ProcessID})
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if !strings.Contains(readResp.Data, "hello") {
		t.Fatalf("unexpected read data: %q", readResp.Data)
	}

	statusResp, err := host.Status(ctx, StatusRequest{SessionID: "run-1", ProcessID: startResp.ProcessID})
	if err != nil {
		t.Fatalf("Status returned error: %v", err)
	}
	if statusResp.StdoutCursor == 0 {
		t.Fatal("expected stdout cursor to advance")
	}

	waitResp, err := host.Wait(ctx, WaitRequest{SessionID: "run-1", ProcessID: startResp.ProcessID, TimeoutMs: 2000})
	if err != nil {
		t.Fatalf("Wait returned error: %v", err)
	}
	if !waitResp.Exited {
		t.Fatal("expected process to exit")
	}
	if !strings.Contains(waitResp.Stdout, "hello") {
		t.Fatalf("unexpected stdout: %q", waitResp.Stdout)
	}
}
