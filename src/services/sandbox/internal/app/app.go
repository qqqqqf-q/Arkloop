package app

import (
"bufio"
"context"
"errors"
"fmt"
"net"
"net/http"
"os"
"os/signal"
"strings"
"syscall"
"time"

"arkloop/services/sandbox/internal/logging"
"arkloop/services/sandbox/internal/session"
)

// LoadDotenvIfEnabled 读取 .env 文件（若存在且未被禁用）。
func LoadDotenvIfEnabled(forceDisable bool) (bool, error) {
if forceDisable {
return false, nil
}
if strings.TrimSpace(os.Getenv("DOTENV_DISABLED")) == "true" {
return false, nil
}
f, err := os.Open(".env")
if err != nil {
if errors.Is(err, os.ErrNotExist) {
return false, nil
}
return false, err
}
defer f.Close()

scanner := bufio.NewScanner(f)
for scanner.Scan() {
line := strings.TrimSpace(scanner.Text())
if line == "" || strings.HasPrefix(line, "#") {
continue
}
key, value, found := strings.Cut(line, "=")
if !found {
continue
}
key = strings.TrimSpace(key)
value = strings.TrimSpace(value)
if _, ok := os.LookupEnv(key); !ok {
_ = os.Setenv(key, value)
}
}
return true, scanner.Err()
}

type Application struct {
config  Config
logger  *logging.JSONLogger
manager *session.Manager
}

// NewApplication 创建 Application 实例，session.Manager 由调用方创建并注入。
func NewApplication(config Config, logger *logging.JSONLogger, manager *session.Manager) (*Application, error) {
if err := config.Validate(); err != nil {
return nil, err
}
if logger == nil {
return nil, fmt.Errorf("logger must not be nil")
}
if manager == nil {
return nil, fmt.Errorf("manager must not be nil")
}
return &Application{config: config, logger: logger, manager: manager}, nil
}

// Run 启动 HTTP 服务器，直到收到信号或 ctx 取消。
// handler 由调用方（main.go）构建后注入，避免循环导入。
func (a *Application) Run(ctx context.Context, handler http.Handler) error {
if ctx == nil {
ctx = context.Background()
}
ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
defer stop()

listener, err := net.Listen("tcp", a.config.Addr)
if err != nil {
return err
}
defer func() { _ = listener.Close() }()

a.logger.Info("sandbox started", logging.LogFields{}, map[string]any{
"addr":            a.config.Addr,
"firecracker_bin": a.config.FirecrackerBin,
"max_sessions":    a.config.MaxSessions,
})

server := &http.Server{
Handler:           handler,
ReadHeaderTimeout: 5 * time.Second,
}

errCh := make(chan error, 1)
go func() {
errCh <- server.Serve(listener)
}()

select {
case <-ctx.Done():
case err := <-errCh:
if err == nil || errors.Is(err, http.ErrServerClosed) {
return nil
}
return err
}

shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()

a.logger.Info("sandbox shutting down", logging.LogFields{}, nil)
if err := server.Shutdown(shutdownCtx); err != nil {
_ = server.Close()
}
a.manager.CloseAll(shutdownCtx)
a.manager.DrainPool(shutdownCtx)

if err, ok := <-errCh; ok && err != nil && !errors.Is(err, http.ErrServerClosed) {
return err
}
return nil
}
