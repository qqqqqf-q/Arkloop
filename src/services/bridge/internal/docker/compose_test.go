package docker

import "testing"

func TestParsePSEntries(t *testing.T) {
	raw := []byte(`{"Service":"gateway","State":"running","Health":"healthy"}
{"Service":"searxng","State":"exited","Health":""}
`)

	entries := parsePSEntries(raw)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	if entries[0].Service != "gateway" || entries[0].State != "running" {
		t.Fatalf("entries[0] = %#v, want gateway/running", entries[0])
	}
	if entries[1].Service != "searxng" || entries[1].State != "exited" {
		t.Fatalf("entries[1] = %#v, want searxng/exited", entries[1])
	}
}

func TestParsePSEntriesSkipsInvalidLines(t *testing.T) {
	raw := []byte(`{"Service":"gateway","State":"running","Health":"healthy"}
not-json
{"Service":"bridge","State":"running","Health":""}
`)

	entries := parsePSEntries(raw)
	if len(entries) != 2 {
		t.Fatalf("len(entries) = %d, want 2", len(entries))
	}

	if entries[0].Service != "gateway" {
		t.Fatalf("entries[0].Service = %q, want gateway", entries[0].Service)
	}
	if entries[1].Service != "bridge" {
		t.Fatalf("entries[1].Service = %q, want bridge", entries[1].Service)
	}
}

func TestMapStatus(t *testing.T) {
	cases := []struct {
		entry psEntry
		want  string
	}{
		{psEntry{State: "running", Health: "healthy"}, "running"},
		{psEntry{State: "running", Health: ""}, "running"},
		{psEntry{State: "running", Health: "unhealthy"}, "error"},
		{psEntry{State: "exited", Health: ""}, "stopped"},
		{psEntry{State: "created", Health: ""}, "stopped"},
		{psEntry{State: "paused", Health: ""}, "stopped"},
		{psEntry{State: "restarting", Health: ""}, "error"},
		{psEntry{State: "Restarting (1)", Health: ""}, "error"},
		{psEntry{State: "dead", Health: ""}, "error"},
		{psEntry{State: "removing", Health: ""}, "error"},
		{psEntry{State: "unknown-state", Health: ""}, "error"},
		{psEntry{State: "", Health: ""}, "not_installed"},
	}
	for _, tc := range cases {
		got := mapStatus(tc.entry)
		if got != tc.want {
			t.Fatalf("mapStatus(%#v) = %q, want %q", tc.entry, got, tc.want)
		}
	}
}
