package pipeline

import "context"

// RunHandler 是 Pipeline 的终端处理函数类型。
type RunHandler func(ctx context.Context, rc *RunContext) error

// RunMiddleware 包裹 RunHandler，实现拦截、前处理和后处理。
type RunMiddleware func(ctx context.Context, rc *RunContext, next RunHandler) error

// Build 将中间件列表和终端 handler 组装为单个可调用的 RunHandler。
// 执行顺序：middlewares[0] → middlewares[1] → ... → terminal。
func Build(middlewares []RunMiddleware, terminal RunHandler) RunHandler {
	h := terminal
	for i := len(middlewares) - 1; i >= 0; i-- {
		mw := middlewares[i]
		next := h
		h = func(ctx context.Context, rc *RunContext) error {
			return mw(ctx, rc, next)
		}
	}
	return h
}
