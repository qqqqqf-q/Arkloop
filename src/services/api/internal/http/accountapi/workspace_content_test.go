package accountapi

import "testing"

func TestBuildWorkspaceFileListReturnsEmptyForEmptyManifest(t *testing.T) {
	items := buildWorkspaceFileList(workspaceManifest{}, "")
	if len(items) != 0 {
		t.Fatalf("expected empty list, got %#v", items)
	}
}

func TestBuildWorkspaceFileListGroupsNestedEntries(t *testing.T) {
	items := buildWorkspaceFileList(workspaceManifest{Entries: []workspaceManifestEntry{
		{Path: "src/main.go", Type: workspaceEntryTypeFile, Size: 14},
		{Path: "docs/readme.md", Type: workspaceEntryTypeFile, Size: 9},
		{Path: "top.txt", Type: workspaceEntryTypeFile, Size: 4},
	}}, "")

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %#v", items)
	}
	if items[0].Type != workspaceEntryTypeDir || items[0].Path != "/docs" {
		t.Fatalf("unexpected first item: %#v", items[0])
	}
	if items[1].Type != workspaceEntryTypeDir || items[1].Path != "/src" {
		t.Fatalf("unexpected second item: %#v", items[1])
	}
	if items[2].Type != workspaceEntryTypeFile || items[2].Path != "/top.txt" {
		t.Fatalf("unexpected third item: %#v", items[2])
	}
	if !items[0].HasChildren || !items[1].HasChildren {
		t.Fatalf("expected nested directories to report children: %#v", items)
	}
	if items[2].MimeType == nil || *items[2].MimeType == "" {
		t.Fatalf("expected file mime type, got %#v", items[2])
	}
}
