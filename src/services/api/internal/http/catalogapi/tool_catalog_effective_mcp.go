package catalogapi

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"arkloop/services/api/internal/data"
	sharedenvironmentref "arkloop/services/shared/environmentref"
	sharedmcpinstall "arkloop/services/shared/mcpinstall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	effectiveToolCatalogMCPGroup = "mcp"
	effectiveMCPConfigFileEnv    = "ARKLOOP_MCP_CONFIG_FILE"
	effectiveMCPDefaultTimeoutMs = 10000
	effectiveMCPProtocolVersion  = "2025-06-18"
	effectiveToolCatalogCacheEnv = "__env__"
)

type effectiveToolCatalogCache struct {
	ttl     time.Duration
	entries sync.Map
}

type effectiveToolCatalogCacheEntry struct {
	tools    []toolCatalogItem
	cachedAt time.Time
}

func newEffectiveToolCatalogCache(ttl time.Duration) *effectiveToolCatalogCache {
	return &effectiveToolCatalogCache{ttl: ttl}
}

func (c *effectiveToolCatalogCache) GetEnv(ctx context.Context) ([]toolCatalogItem, error) {
	return c.get(ctx, effectiveToolCatalogCacheEnv, func(context.Context) ([]toolCatalogItem, error) {
		servers, err := loadEffectiveMCPConfigFromEnv()
		if err != nil {
			return nil, err
		}
		return discoverEffectiveMCPTools(ctx, servers)
	})
}

func (c *effectiveToolCatalogCache) GetAccount(ctx context.Context, pool data.DB, accountID uuid.UUID, userID uuid.UUID) ([]toolCatalogItem, error) {
	if accountID == uuid.Nil || userID == uuid.Nil {
		return nil, nil
	}
	cacheKey := accountID.String() + "|" + userID.String()
	return c.get(ctx, cacheKey, func(context.Context) ([]toolCatalogItem, error) {
		servers, err := loadEffectiveMCPConfigFromDB(ctx, pool, accountID, userID)
		if err != nil {
			return nil, err
		}
		return discoverEffectiveMCPTools(ctx, servers)
	})
}

func (c *effectiveToolCatalogCache) get(ctx context.Context, key string, load func(context.Context) ([]toolCatalogItem, error)) ([]toolCatalogItem, error) {
	if c == nil || c.ttl <= 0 {
		return load(ctx)
	}
	if raw, ok := c.entries.Load(key); ok {
		entry := raw.(effectiveToolCatalogCacheEntry)
		if time.Since(entry.cachedAt) < c.ttl {
			return cloneToolCatalogItems(entry.tools), nil
		}
	}
	tools, err := load(ctx)
	if err != nil {
		return nil, err
	}
	c.entries.Store(key, effectiveToolCatalogCacheEntry{tools: cloneToolCatalogItems(tools), cachedAt: time.Now()})
	return tools, nil
}

func (c *effectiveToolCatalogCache) Invalidate(key string) {
	if c == nil {
		return
	}
	prefix := strings.TrimSpace(key)
	if prefix == "" {
		return
	}
	c.entries.Range(func(key, _ any) bool {
		text, ok := key.(string)
		if ok && (text == prefix || strings.HasPrefix(text, prefix+"|")) {
			c.entries.Delete(key)
		}
		return true
	})
}

func (c *effectiveToolCatalogCache) StartInvalidationListener(ctx context.Context, directPool *pgxpool.Pool) {
	if c == nil || directPool == nil || c.ttl <= 0 {
		return
	}
	go c.listenForInvalidation(ctx, directPool)
}

func (c *effectiveToolCatalogCache) listenForInvalidation(ctx context.Context, directPool *pgxpool.Pool) {
	const (
		baseDelay = time.Second
		maxDelay  = 30 * time.Second
	)
	delay := baseDelay
	for {
		if ctx.Err() != nil {
			return
		}
		err := c.listenOnce(ctx, directPool)
		if ctx.Err() != nil {
			return
		}
		_ = err
		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}

func (c *effectiveToolCatalogCache) listenOnce(ctx context.Context, directPool *pgxpool.Pool) error {
	conn, err := directPool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "LISTEN mcp_config_changed"); err != nil {
		return err
	}

	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		payload := strings.TrimSpace(n.Payload)
		if payload == "" {
			continue
		}
		c.Invalidate(payload)
	}
}

type effectiveMCPServerConfig = sharedmcpinstall.ServerConfig

type effectiveMCPTool struct {
	Name        string
	Title       *string
	Description *string
}

func loadEffectiveMCPConfigFromEnv() ([]effectiveMCPServerConfig, error) {
	raw := strings.TrimSpace(os.Getenv(effectiveMCPConfigFileEnv))
	if raw == "" {
		return nil, nil
	}
	path := expandUserPath(raw)
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("mcp effective catalog: config file not found: %s", raw)
	}

	var parsed any
	if err := json.Unmarshal(content, &parsed); err != nil {
		return nil, fmt.Errorf("mcp effective catalog: config file is not valid json")
	}
	root, ok := parsed.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mcp effective catalog: config root must be an object")
	}
	rawServers := root["mcpServers"]
	if rawServers == nil {
		rawServers = root["mcp_servers"]
	}
	if rawServers == nil {
		return nil, nil
	}
	serverMap, ok := rawServers.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mcp effective catalog: mcpServers must be an object")
	}
	serverIDs := make([]string, 0, len(serverMap))
	for serverID := range serverMap {
		serverIDs = append(serverIDs, serverID)
	}
	sort.Strings(serverIDs)

	servers := make([]effectiveMCPServerConfig, 0, len(serverIDs))
	for _, serverID := range serverIDs {
		payload, ok := serverMap[serverID].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("mcp effective catalog: server %q must be an object", serverID)
		}
		server, err := parseEffectiveMCPServerConfig(serverID, payload)
		if err != nil {
			return nil, err
		}
		servers = append(servers, server)
	}
	return servers, nil
}

func loadEffectiveMCPConfigFromDB(ctx context.Context, pool data.DB, accountID uuid.UUID, userID uuid.UUID) ([]effectiveMCPServerConfig, error) {
	if pool == nil || accountID == uuid.Nil || userID == uuid.Nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	profileRepo, err := data.NewProfileRegistriesRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("mcp effective catalog: init profile repo: %w", err)
	}
	workspaceRepo, err := data.NewWorkspaceRegistriesRepository(pool)
	if err != nil {
		return nil, fmt.Errorf("mcp effective catalog: init workspace repo: %w", err)
	}
	profileRef := sharedenvironmentref.BuildProfileRef(accountID, &userID)
	workspaceRef, err := ensureDefaultWorkspaceForProfile(ctx, profileRepo, workspaceRepo, accountID, userID, profileRef)
	if err != nil {
		return nil, fmt.Errorf("mcp effective catalog: resolve workspace: %w", err)
	}
	if strings.TrimSpace(workspaceRef) == "" {
		return nil, nil
	}
	installs, err := sharedmcpinstall.LoadEnabledInstalls(ctx, pool, accountID, profileRef, workspaceRef)
	if err != nil {
		return nil, fmt.Errorf("mcp effective catalog: load enabled installs: %w", err)
	}

	var (
		keyRing    catalogKeyRing
		keyRingErr error
	)
	decrypt := func(_ context.Context, encrypted string, keyVersion *int) ([]byte, error) {
		if keyVersion == nil {
			return nil, fmt.Errorf("tool_catalog_effective_mcp: missing key version")
		}
		if keyRing == nil && keyRingErr == nil {
			keyRing, keyRingErr = newEffectiveCatalogKeyRing()
		}
		if keyRingErr != nil {
			return nil, keyRingErr
		}
		if keyRing == nil {
			return nil, fmt.Errorf("tool_catalog_effective_mcp: key ring missing")
		}
		return keyRing.Decrypt(encrypted, *keyVersion)
	}

	servers := sharedmcpinstall.ServerConfigsFromInstalls(ctx, installs, decrypt, effectiveMCPDefaultTimeoutMs)
	return servers, nil
}

func parseEffectiveMCPServerConfig(serverID string, payload map[string]any) (effectiveMCPServerConfig, error) {
	return sharedmcpinstall.ParseServerConfig(serverID, payload, effectiveMCPDefaultTimeoutMs)
}

func discoverEffectiveMCPTools(ctx context.Context, servers []effectiveMCPServerConfig) ([]toolCatalogItem, error) {
	if len(servers) == 0 {
		return nil, nil
	}
	type discovered struct {
		server effectiveMCPServerConfig
		tools  []effectiveMCPTool
	}
	discoveredByServer := make([]discovered, 0, len(servers))
	baseCounts := map[string]int{}
	for _, server := range servers {
		tools, err := listEffectiveMCPServerTools(ctx, server)
		if err != nil || len(tools) == 0 {
			continue
		}
		discoveredByServer = append(discoveredByServer, discovered{server: server, tools: tools})
		for _, tool := range tools {
			base := effectiveMCPToolBaseName(server.ServerID, tool.Name)
			baseCounts[base]++
		}
	}
	usedNames := map[string]struct{}{}
	items := []toolCatalogItem{}
	for _, entry := range discoveredByServer {
		for _, tool := range entry.tools {
			base := effectiveMCPToolBaseName(entry.server.ServerID, tool.Name)
			internalName := base
			if baseCounts[base] > 1 {
				internalName = base + "__" + effectiveMCPShortHash(effectiveMCPToolRawName(entry.server.ServerID, tool.Name))
			}
			internalName = ensureEffectiveMCPUniqueToolName(internalName, usedNames)

			label := strings.TrimSpace(tool.Name)
			if tool.Title != nil && strings.TrimSpace(*tool.Title) != "" {
				label = strings.TrimSpace(*tool.Title)
			}
			description := "MCP tool: " + strings.TrimSpace(tool.Name)
			if tool.Description != nil && strings.TrimSpace(*tool.Description) != "" {
				description = strings.TrimSpace(*tool.Description)
			} else if tool.Title != nil && strings.TrimSpace(*tool.Title) != "" {
				description = strings.TrimSpace(*tool.Title)
			}

			items = append(items, toolCatalogItem{
				Name:              internalName,
				Label:             label,
				LLMDescription:    description,
				HasOverride:       false,
				DescriptionSource: toolDescriptionSourceDefault,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	return items, nil
}

func listEffectiveMCPServerTools(ctx context.Context, server effectiveMCPServerConfig) ([]effectiveMCPTool, error) {
	switch server.Transport {
	case "http_sse", "streamable_http":
		return listEffectiveMCPHTTPTools(ctx, server)
	case "stdio":
		return listEffectiveMCPStdioTools(ctx, server)
	default:
		return nil, fmt.Errorf("mcp effective catalog: transport not supported")
	}
}

func listEffectiveMCPHTTPTools(ctx context.Context, server effectiveMCPServerConfig) ([]effectiveMCPTool, error) {
	if _, err := url.Parse(server.URL); err != nil {
		return nil, err
	}
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]any{},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(server.CallTimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = time.Duration(effectiveMCPDefaultTimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for key, value := range server.Headers {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		req.Header.Set(strings.TrimSpace(key), strings.TrimSpace(value))
	}
	resp, err := newEffectiveMCPHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("mcp effective catalog: server returned %d", resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	var payload map[string]any
	if strings.Contains(contentType, "text/event-stream") {
		payload, err = parseEffectiveMCPSSEResponse(resp.Body)
	} else {
		err = json.NewDecoder(resp.Body).Decode(&payload)
	}
	if err != nil {
		return nil, err
	}
	result, err := parseEffectiveMCPResponse(payload)
	if err != nil {
		return nil, err
	}
	return parseEffectiveMCPTools(result), nil
}

func listEffectiveMCPStdioTools(ctx context.Context, server effectiveMCPServerConfig) ([]effectiveMCPTool, error) {
	client := newEffectiveMCPStdioClient(server)
	defer func() { _ = client.Close() }()
	return client.ListTools(ctx)
}

type effectiveMCPStdioClient struct {
	server      effectiveMCPServerConfig
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	closed      bool
	nextID      int64
	pending     map[int64]chan map[string]any
	mu          sync.Mutex
	writeMu     sync.Mutex
	initialized bool
}

func newEffectiveMCPStdioClient(server effectiveMCPServerConfig) *effectiveMCPStdioClient {
	return &effectiveMCPStdioClient{server: server, nextID: 1, pending: map[int64]chan map[string]any{}}
}

func (c *effectiveMCPStdioClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	cmd := c.cmd
	stdin := c.stdin
	stdout := c.stdout
	pending := c.pending
	c.pending = map[int64]chan map[string]any{}
	c.mu.Unlock()
	for _, ch := range pending {
		close(ch)
	}
	if stdin != nil {
		_ = stdin.Close()
	}
	if stdout != nil {
		_ = stdout.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	return nil
}

func (c *effectiveMCPStdioClient) ListTools(ctx context.Context) ([]effectiveMCPTool, error) {
	if err := c.initialize(ctx); err != nil {
		return nil, err
	}
	result, err := c.request(ctx, "tools/list", map[string]any{}, c.server.CallTimeoutMs)
	if err != nil {
		return nil, err
	}
	return parseEffectiveMCPTools(result), nil
}

func (c *effectiveMCPStdioClient) initialize(ctx context.Context) error {
	c.mu.Lock()
	if c.initialized {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	if err := c.ensureStarted(ctx); err != nil {
		return err
	}
	if _, err := c.request(ctx, "initialize", map[string]any{
		"protocolVersion": effectiveMCPProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "arkloop-api", "version": "0"},
	}, c.server.CallTimeoutMs); err != nil {
		return err
	}
	if err := c.notify(ctx, "notifications/initialized", nil); err != nil {
		return err
	}
	c.mu.Lock()
	c.initialized = true
	c.mu.Unlock()
	return nil
}

func (c *effectiveMCPStdioClient) ensureStarted(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("mcp effective catalog: client closed")
	}
	if c.cmd != nil {
		c.mu.Unlock()
		return nil
	}
	server := c.server
	c.mu.Unlock()
	cmd := exec.CommandContext(ctx, server.Command, server.Args...)
	if server.Cwd != nil {
		cmd.Dir = *server.Cwd
	}
	cmd.Env = buildEffectiveMCPEnv(server)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return err
	}
	c.mu.Lock()
	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout
	c.mu.Unlock()
	go c.readLoop(stdout)
	go func() {
		_, _ = io.Copy(io.Discard, stderr)
	}()
	return nil
}

func (c *effectiveMCPStdioClient) request(ctx context.Context, method string, params map[string]any, timeoutMs int) (map[string]any, error) {
	if err := c.ensureStarted(ctx); err != nil {
		return nil, err
	}
	id := c.reserveID()
	ch := make(chan map[string]any, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, fmt.Errorf("mcp effective catalog: client closed")
	}
	c.pending[id] = ch
	stdin := c.stdin
	c.mu.Unlock()
	payload := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		payload["params"] = params
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	c.writeMu.Lock()
	_, err = stdin.Write(append(encoded, '\n'))
	c.writeMu.Unlock()
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(timeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = time.Duration(effectiveMCPDefaultTimeoutMs) * time.Millisecond
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, fmt.Errorf("mcp effective catalog: request timed out")
	case resp, ok := <-ch:
		if !ok {
			return nil, fmt.Errorf("mcp effective catalog: client disconnected")
		}
		return parseEffectiveMCPResponse(resp)
	}
}

func (c *effectiveMCPStdioClient) notify(ctx context.Context, method string, params map[string]any) error {
	if err := c.ensureStarted(ctx); err != nil {
		return err
	}
	c.mu.Lock()
	stdin := c.stdin
	c.mu.Unlock()
	payload := map[string]any{"jsonrpc": "2.0", "method": method}
	if params != nil {
		payload["params"] = params
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	_, err = stdin.Write(append(encoded, '\n'))
	c.writeMu.Unlock()
	return err
}

func (c *effectiveMCPStdioClient) reserveID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	return id
}

func (c *effectiveMCPStdioClient) readLoop(stdout io.Reader) {
	reader := bufio.NewReader(stdout)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			_ = c.Close()
			return
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
			continue
		}
		id, ok := parseEffectiveMCPID(payload["id"])
		if !ok {
			continue
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch == nil {
			continue
		}
		ch <- payload
		close(ch)
	}
}

func parseEffectiveMCPTools(result map[string]any) []effectiveMCPTool {
	rawTools := result["tools"]
	if rawTools == nil {
		return nil
	}
	list, ok := rawTools.([]any)
	if !ok {
		return nil
	}
	tools := make([]effectiveMCPTool, 0, len(list))
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := strings.TrimSpace(effectiveMCPAsString(obj["name"]))
		if name == "" {
			continue
		}
		tools = append(tools, effectiveMCPTool{Name: name, Title: effectiveMCPOptionalString(obj["title"]), Description: effectiveMCPOptionalString(obj["description"])})
	}
	return tools
}

func parseEffectiveMCPResponse(payload map[string]any) (map[string]any, error) {
	if rawErr, ok := payload["error"].(map[string]any); ok {
		return nil, fmt.Errorf("mcp effective catalog: %s", strings.TrimSpace(effectiveMCPAsString(rawErr["message"])))
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mcp effective catalog: missing result")
	}
	return result, nil
}

func parseEffectiveMCPSSEResponse(r io.Reader) (map[string]any, error) {
	scanner := bufio.NewScanner(r)
	var dataLine string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			dataLine = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			continue
		}
		if line == "" && dataLine != "" {
			var payload map[string]any
			if err := json.Unmarshal([]byte(dataLine), &payload); err == nil {
				return payload, nil
			}
			dataLine = ""
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("mcp effective catalog: empty sse response")
}

func newEffectiveMCPHTTPClient() *http.Client {
	return &http.Client{}
}

func parseEffectiveMCPID(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), typed > 0
	case int:
		return int64(typed), typed > 0
	case int64:
		return typed, typed > 0
	default:
		return 0, false
	}
}

func buildEffectiveMCPEnv(server effectiveMCPServerConfig) []string {
	env := make([]string, 0, len(server.Env))
	for key, value := range server.Env {
		env = append(env, fmt.Sprintf("%s=%s", key, value))
	}
	return env
}

func expandUserPath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func effectiveMCPToolRawName(serverID string, toolName string) string {
	return "mcp__" + serverID + "__" + toolName
}

func effectiveMCPToolBaseName(serverID string, toolName string) string {
	raw := effectiveMCPToolRawName(serverID, toolName)
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '_':
			return r
		default:
			return '_'
		}
	}, raw)
	cleaned = strings.Trim(cleaned, "_")
	if cleaned == "" {
		return "mcp_tool"
	}
	return cleaned
}

func effectiveMCPShortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}

func ensureEffectiveMCPUniqueToolName(name string, used map[string]struct{}) string {
	if _, ok := used[name]; !ok {
		used[name] = struct{}{}
		return name
	}
	index := 2
	for {
		candidate := name + "_" + strconv.Itoa(index)
		if _, ok := used[candidate]; !ok {
			used[candidate] = struct{}{}
			return candidate
		}
		index++
	}
}

func cloneToolCatalogItems(items []toolCatalogItem) []toolCatalogItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]toolCatalogItem, len(items))
	copy(out, items)
	return out
}

func effectiveMCPAsString(value any) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func effectiveMCPOptionalString(value any) *string {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	cleaned := strings.TrimSpace(text)
	if cleaned == "" {
		return nil
	}
	return &cleaned
}
