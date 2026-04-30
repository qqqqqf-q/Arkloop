package main

import (
	"context"
	"testing"

	"arkloop/services/cli/internal/apiclient"
)

func TestRouteCommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want commandRoute
	}{
		{name: "--version", args: []string{"--version"}, want: commandVersion},
		{name: "version", args: []string{"version"}, want: commandVersion},
		{name: "run", args: []string{"run", "hello"}, want: commandRun},
		{name: "chat", args: []string{"chat"}, want: commandChat},
		{name: "status", args: []string{"status"}, want: commandStatus},
		{name: "models list", args: []string{"models", "list"}, want: commandModelsList},
		{name: "personas list", args: []string{"personas", "list"}, want: commandPersonasList},
		{name: "sessions list", args: []string{"sessions", "list"}, want: commandSessionsList},
		{name: "sessions resume", args: []string{"sessions", "resume", "abc"}, want: commandSessionsChat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := routeCommand(tt.args)
			if err != nil {
				t.Fatalf("routeCommand: %v", err)
			}
			if got.kind != tt.want {
				t.Fatalf("expected %s, got %s", tt.want, got.kind)
			}
		})
	}
}

func TestRouteCommandRejectsUnknownCommand(t *testing.T) {
	if _, err := routeCommand([]string{"sessions", "open"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestCmdSessionsResumeRequiresID(t *testing.T) {
	err := cmdSessionsResume(context.Background(), nil)
	if err == nil {
		t.Fatal("expected usage error")
	}
	ee, ok := err.(*exitError)
	if !ok || ee.code != 2 {
		t.Fatalf("expected exitError code 2, got %#v", err)
	}
}

func TestModelViewsFromProvidersSortsStable(t *testing.T) {
	views := modelViewsFromProviders([]apiclient.LlmProvider{
		{
			ID:   "provider-2",
			Name: "B",
			Models: []apiclient.ProviderModel{
				{Model: "gpt-4.1", ProviderID: "provider-2", IsDefault: false, ShowInPicker: true},
			},
		},
		{
			ID:   "provider-1",
			Name: "A",
			Models: []apiclient.ProviderModel{
				{Model: "gpt-4.1-mini", ProviderID: "provider-1", IsDefault: true, ShowInPicker: true},
				{Model: "gpt-4.1-nano", ProviderID: "provider-1", IsDefault: false, ShowInPicker: false},
			},
		},
	})

	if len(views) != 3 {
		t.Fatalf("unexpected view count: %d", len(views))
	}
	if views[0].Model != "gpt-4.1-mini" || views[1].ProviderName != "B" || views[2].Model != "gpt-4.1-nano" {
		t.Fatalf("unexpected order: %#v", views)
	}
}

func TestPersonaViewsSortsBySelectorOrderAndName(t *testing.T) {
	views := personaViews([]apiclient.Persona{
		{PersonaKey: "ops", DisplayName: "Ops", SelectorOrder: 99},
		{PersonaKey: "search", DisplayName: "Search", SelectorName: "Search", SelectorOrder: 1},
		{PersonaKey: "alpha", DisplayName: "Alpha", SelectorOrder: 1},
	})

	if len(views) != 3 {
		t.Fatalf("unexpected view count: %d", len(views))
	}
	if views[0].PersonaKey != "alpha" || views[1].PersonaKey != "search" || views[2].PersonaKey != "ops" {
		t.Fatalf("unexpected order: %#v", views)
	}
}
