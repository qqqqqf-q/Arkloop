package agentdirectory

import "context"

type FileContent struct {
	Path    string
	Content string
}

// Content 从 agent work directory 读取的文件内容，空字符串表示文件不存在。
type Content struct {
	Soul             string // SOUL.md
	Instructions     string // AGENTS.md
	Memory           string // MEMORY.md
	User             string // USER.md
	Bootstrap        string // BOOTSTRAP.md
	Identity         string // IDENTITY.md
	Tools            string // TOOLS.md
	Heartbeat        string // HEARTBEAT.md
	ExtraFiles       []FileContent
	DailyMemoryFiles []FileContent  // memory/YYYY-MM-DD.md 最近日志
	WorkDirPath      string         // AWD 路径，注入到 system prompt
	BootstrapPending bool           // BOOTSTRAP.md 存在且尚未完成
}

// Provider 读取 agent work directory 内容。
type Provider interface {
	Load(ctx context.Context, profileRef string) (*Content, error)
}
