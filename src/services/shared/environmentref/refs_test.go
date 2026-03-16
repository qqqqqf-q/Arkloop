package environmentref

import (
	"testing"

	"github.com/google/uuid"
)

func TestBuildProfileRefStablePerUser(t *testing.T) {
	orgID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	userID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	first := BuildProfileRef(orgID, &userID)
	second := BuildProfileRef(orgID, &userID)
	otherUser := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	third := BuildProfileRef(orgID, &otherUser)

	if first != second {
		t.Fatalf("expected stable profile_ref, got %q vs %q", first, second)
	}
	if first != "pref_3045a41dbf7ca01a9a72827228072d39" {
		t.Fatalf("unexpected profile_ref: %q", first)
	}
	if first == third {
		t.Fatalf("expected different profile_ref for different users, got %q", first)
	}
}

func TestBuildWorkspaceRefStableByBinding(t *testing.T) {
	orgID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	projectID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	threadID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	profileRef := "pref_3045a41dbf7ca01a9a72827228072d39"

	projectRef := BuildWorkspaceRef(orgID, profileRef, "project", projectID)
	projectRef2 := BuildWorkspaceRef(orgID, profileRef, "project", projectID)
	threadRef := BuildWorkspaceRef(orgID, profileRef, "thread", threadID)

	if projectRef != projectRef2 {
		t.Fatalf("expected stable workspace_ref, got %q vs %q", projectRef, projectRef2)
	}
	if projectRef != "wsref_8398feadc3eb164d4079c8b41257dac1" {
		t.Fatalf("unexpected project workspace_ref: %q", projectRef)
	}
	if threadRef != "wsref_d7b99922b2a72282c7b7e27ddf0e6742" {
		t.Fatalf("unexpected thread workspace_ref: %q", threadRef)
	}
	if projectRef == threadRef {
		t.Fatalf("expected different workspace_ref for different bindings, got %q", projectRef)
	}
}
