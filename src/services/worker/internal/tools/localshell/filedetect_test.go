//go:build desktop

package localshell

import (
	"testing"
)

func TestDetectRedirectTargets(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
	}{
		{"simple redirect", "echo hello > /tmp/out.txt", []string{"/tmp/out.txt"}},
		{"append redirect", "echo hello >> /tmp/out.txt", []string{"/tmp/out.txt"}},
		{"multiple redirects", "echo a > /tmp/a.txt && echo b > /tmp/b.txt", []string{"/tmp/a.txt", "/tmp/b.txt"}},
		{"no redirect", "echo hello", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectRedirectTargets(tt.command)
			assertPaths(t, got, tt.want)
		})
	}
}

func TestDetectSedInPlace(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
	}{
		{"sed -i with file", "sed -i 's/foo/bar/' /tmp/file.txt", []string{"/tmp/file.txt"}},
		{"sed -i multiple files", "sed -i 's/foo/bar/' a.txt b.txt", []string{"a.txt", "b.txt"}},
		{"sed without -i", "sed 's/foo/bar/' /tmp/file.txt", nil},
		{"sed --in-place", "sed --in-place 's/a/b/' config.yaml", []string{"config.yaml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectSedInPlace(tt.command)
			assertPaths(t, got, tt.want)
		})
	}
}

func TestDetectTeeTargets(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
	}{
		{"simple tee", "echo hello | tee /tmp/out.txt", []string{"/tmp/out.txt"}},
		{"tee -a", "echo hello | tee -a /tmp/out.txt", []string{"/tmp/out.txt"}},
		{"tee multiple", "echo hello | tee a.txt b.txt", []string{"a.txt", "b.txt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectTeeTargets(tt.command)
			assertPaths(t, got, tt.want)
		})
	}
}

func TestDetectCpMvTargets(t *testing.T) {
	tests := []struct {
		name    string
		command string
		want    []string
	}{
		{"cp", "cp src.txt dst.txt", []string{"dst.txt"}},
		{"mv", "mv old.txt new.txt", []string{"new.txt"}},
		{"cp with flag", "cp -r srcdir dstdir", []string{"dstdir"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectCpMvTargets(tt.command)
			assertPaths(t, got, tt.want)
		})
	}
}

func TestDetectModifiedFiles(t *testing.T) {
	tests := []struct {
		name    string
		command string
		cwd     string
		want    []string
	}{
		{
			"redirect with relative path",
			"echo test > out.txt",
			"/home/user/project",
			[]string{"/home/user/project/out.txt"},
		},
		{
			"absolute path unchanged",
			"echo test > /tmp/out.txt",
			"/home/user",
			[]string{"/tmp/out.txt"},
		},
		{
			"dedup",
			"echo a > /tmp/f.txt && echo b > /tmp/f.txt",
			"",
			[]string{"/tmp/f.txt"},
		},
		{
			"mixed commands",
			"sed -i 's/a/b/' config.yaml && cp config.yaml backup.yaml",
			"/app",
			[]string{"/app/config.yaml", "/app/backup.yaml"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectModifiedFiles(tt.command, tt.cwd)
			assertPaths(t, got, tt.want)
		})
	}
}

func assertPaths(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("got %v, want %v", got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
