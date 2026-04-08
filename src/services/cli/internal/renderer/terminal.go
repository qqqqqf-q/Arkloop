package renderer

import (
	"fmt"
	"io"

	"arkloop/services/cli/internal/sse"
)

// Renderer 将 SSE 事件流式渲染到终端。
type Renderer struct {
	out     io.Writer
	started bool
}

// NewRenderer 构造渲染器。
func NewRenderer(out io.Writer) *Renderer {
	return &Renderer{out: out}
}

// OnEvent 根据事件类型写入终端输出。
func (r *Renderer) OnEvent(e sse.Event) {
	switch e.Type {
	case "message.delta":
		s, _ := e.Data["content_delta"].(string)
		if s == "" {
			return
		}
		fmt.Fprint(r.out, s)
		r.started = true

	case "tool.call":
		if r.started {
			fmt.Fprintln(r.out)
		}
		fmt.Fprintf(r.out, "[tool: %s]\n", e.ToolName)
		r.started = false

	case "tool.result":
		fmt.Fprintf(r.out, "[result: %s]\n", e.ToolName)
		r.started = false

	case "run.completed":
		if r.started {
			fmt.Fprintln(r.out)
		}
		r.started = false

	case "run.failed":
		if r.started {
			fmt.Fprintln(r.out)
		}
		msg, _ := e.Data["error"].(string)
		if msg == "" {
			msg = "unknown error"
		}
		fmt.Fprintf(r.out, "error: %s\n", msg)
		r.started = false

	case "run.cancelled":
		if r.started {
			fmt.Fprintln(r.out)
		}
		fmt.Fprintln(r.out, "cancelled")
		r.started = false
	}
}

// Flush 确保终端光标在新行。
func (r *Renderer) Flush() {
	if r.started {
		fmt.Fprintln(r.out)
		r.started = false
	}
}
