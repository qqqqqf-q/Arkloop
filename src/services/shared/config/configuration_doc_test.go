package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestConfigurationDocUpToDate(t *testing.T) {
	repoRoot := repoRootFromThisFile(t)
	docPath := filepath.Join(repoRoot, "docs", "reference", "configuration.md")
	genPath := filepath.Join(repoRoot, "src", "services", "shared", "cmd", "configdoc", "main.go")

	want := RenderConfigurationMarkdown(DefaultRegistry())
	got, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("读取配置文档失败: %v", err)
	}

	if string(got) != want {
		t.Fatalf("配置文档未同步，请重新生成:\n  go run %s -out %s", genPath, docPath)
	}
}

func repoRootFromThisFile(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位当前文件路径")
	}

	dir := filepath.Dir(file)
	root := filepath.Clean(filepath.Join(dir, "../../../.."))
	return root
}
