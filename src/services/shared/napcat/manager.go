package napcat

import (
	"archive/zip"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	githubReleaseAPI = "https://api.github.com/repos/NapNeko/NapCatQQ/releases/latest"
)

func shellAssetName() string {
	if runtime.GOOS == "windows" {
		return "NapCat.Shell.Windows.Node.zip"
	}
	return "NapCat.Shell.zip"
}

type SetupPhase string

const (
	SetupPhaseNone        SetupPhase = ""
	SetupPhaseFetchInfo   SetupPhase = "fetch_info"
	SetupPhaseDownloading SetupPhase = "downloading"
	SetupPhaseExtracting  SetupPhase = "extracting"
	SetupPhaseStarting    SetupPhase = "starting"
	SetupPhaseDone        SetupPhase = "done"
	SetupPhaseError       SetupPhase = "error"
)

type Status struct {
	Platform       string              `json:"platform"`
	Installed      bool                `json:"installed"`
	Running        bool                `json:"running"`
	LoggedIn       bool                `json:"logged_in"`
	QQ             string              `json:"qq,omitempty"`
	Nickname       string              `json:"nickname,omitempty"`
	QRCodeURL      string              `json:"qrcode_url,omitempty"`
	QRCodeTextURL  string              `json:"qrcode_text_url,omitempty"`
	LoginError     string              `json:"login_error,omitempty"`
	QuickLoginList []QuickLoginAccount `json:"quick_login_list,omitempty"`
	Version        string              `json:"version,omitempty"`
	SetupPhase     SetupPhase          `json:"setup_phase,omitempty"`
	SetupProgress  int64               `json:"setup_progress,omitempty"`
	SetupTotal     int64               `json:"setup_total,omitempty"`
	SetupError     string              `json:"setup_error,omitempty"`
	Logs           []string            `json:"logs,omitempty"`
	OneBotWSURL    string              `json:"onebot_ws_url,omitempty"`
	OneBotHTTPURL  string              `json:"onebot_http_url,omitempty"`
}

type QuickLoginAccount struct {
	Uin      string `json:"uin"`
	Nickname string `json:"nickname"`
	FaceURL  string `json:"face_url,omitempty"`
}

type Manager struct {
	mu         sync.Mutex
	dataDir    string
	installDir string
	cmd        *exec.Cmd
	cancel     context.CancelFunc
	webuiPort  int
	webuiToken string // 写入 webui.json 的原始 token
	wsPort     int
	wsToken    string
	version    string
	logger     *slog.Logger

	// OneBot HTTP Server（Arkloop 通过它发出站消息）
	httpServerPort  int
	httpServerToken string

	// OneBot HTTP Client 回调 token（NapCat 主动 POST 到 Arkloop）
	httpCallbackToken string

	// Arkloop API 监听端口，用于构造回调 URL
	apiPort int

	// NapCat WebUI 鉴权凭证（base64 编码的 credential JSON）
	webuiCredential string

	// shared http clients
	longClient  *http.Client // downloads
	shortClient *http.Client // webui calls

	// log ring buffer (separate lock to avoid contention with mu)
	logMu  sync.Mutex
	logBuf []string

	// setup progress (lock-free reads)
	setupPhase    atomic.Value // SetupPhase
	setupProgress atomic.Int64
	setupTotal    atomic.Int64
	setupError    atomic.Value // string
}

// pidFilePath 返回存放 NapCat 子进程 PID 的文件路径
func (m *Manager) pidFilePath() string {
	return filepath.Join(m.dataDir, "napcat.pid")
}

// savePID 将 PID 写入文件
func (m *Manager) savePID(pid int) {
	_ = os.MkdirAll(m.dataDir, 0755)
	_ = os.WriteFile(m.pidFilePath(), []byte(strconv.Itoa(pid)), 0644)
}

// removePID 删除 PID 文件
func (m *Manager) removePID() {
	_ = os.Remove(m.pidFilePath())
}

// cleanStaleProcess 清理上次残留的 NapCat 进程。
// 通过 PID 文件找到进程，验证它确实是 node 进程后再 kill。
func (m *Manager) cleanStaleProcess() {
	data, err := os.ReadFile(m.pidFilePath())
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		m.removePID()
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		m.removePID()
		return
	}

	// 验证进程是否存活：发送信号 0 不影响进程，仅检查是否存在
	if !isProcessAlive(pid) {
		m.removePID()
		return
	}

	m.logger.Info("napcat: killing stale process", "pid", pid)
	_ = proc.Kill()
	// 等一小段让进程退出
	done := make(chan struct{})
	go func() {
		proc.Wait() //nolint:errcheck
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
	}
	m.removePID()
	m.appendLog(fmt.Sprintf("killed stale napcat process (pid %d)", pid))
}

func NewManager(dataDir string, logger *slog.Logger, apiPort int) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	m := &Manager{
		dataDir:     dataDir,
		installDir:  filepath.Join(dataDir, "shell"),
		apiPort:     apiPort,
		longClient:  &http.Client{Timeout: 10 * time.Minute},
		shortClient: &http.Client{Timeout: 5 * time.Second},
		logger:      logger,
		logBuf:      make([]string, 0, 200),
	}
	m.setupPhase.Store(SetupPhaseNone)
	m.setupError.Store("")
	return m
}

func (m *Manager) Status() Status {
	m.mu.Lock()

	s := Status{
		Platform:      runtime.GOOS,
		Installed:     m.isInstalled(),
		Running:       m.cmd != nil && m.cmd.Process != nil,
		Version:       m.version,
		SetupPhase:    m.setupPhase.Load().(SetupPhase),
		SetupProgress: m.setupProgress.Load(),
		SetupTotal:    m.setupTotal.Load(),
		SetupError:    m.setupError.Load().(string),
	}

	// snapshot logs
	m.logMu.Lock()
	if len(m.logBuf) > 0 {
		s.Logs = make([]string, len(m.logBuf))
		copy(s.Logs, m.logBuf)
	}
	m.logMu.Unlock()

	wsPort := m.wsPort
	httpPort := m.httpServerPort
	m.mu.Unlock()

	if s.Running {
		login := m.checkLoginStatus()
		s.LoggedIn = login.IsLogin || login.IsOffline
		s.QRCodeTextURL = login.QRCodeURL
		s.LoginError = login.LoginError
		if fi, err := os.Stat(m.qrcodePNGPath()); err == nil {
			s.QRCodeURL = fmt.Sprintf("/v1/napcat/qrcode.png?v=%d", fi.ModTime().UnixMilli())
		}
		if s.LoggedIn {
			info := m.getLoginInfo()
			s.QQ = info.QQ
			s.Nickname = info.Nickname
			s.OneBotWSURL = fmt.Sprintf("ws://127.0.0.1:%d", wsPort)
			s.OneBotHTTPURL = fmt.Sprintf("http://127.0.0.1:%d", httpPort)
		} else {
			s.QuickLoginList = m.getQuickLoginList()
		}
	}
	return s
}

// Setup triggers background download (if needed) + start.
func (m *Manager) Setup() error {
	phase := m.setupPhase.Load().(SetupPhase)
	if phase == SetupPhaseFetchInfo || phase == SetupPhaseDownloading || phase == SetupPhaseExtracting || phase == SetupPhaseStarting {
		return fmt.Errorf("napcat: setup already in progress")
	}

	m.setupPhase.Store(SetupPhaseFetchInfo)
	m.setupProgress.Store(0)
	m.setupTotal.Store(0)
	m.setupError.Store("")

	go m.runSetup()
	return nil
}

func (m *Manager) runSetup() {
	ctx := context.Background()

	if m.isInstalled() {
		m.appendLog("napcat: already installed, skipping download")
	} else {
		if err := m.download(ctx); err != nil {
			m.logger.Error("napcat: download failed", "err", err)
			m.setupError.Store(err.Error())
			m.setupPhase.Store(SetupPhaseError)
			return
		}
	}

	m.setupPhase.Store(SetupPhaseStarting)
	if err := m.Start(ctx); err != nil {
		m.logger.Error("napcat: start failed", "err", err)
		m.setupError.Store(err.Error())
		m.setupPhase.Store(SetupPhaseError)
		return
	}

	m.setupPhase.Store(SetupPhaseDone)
}

func (m *Manager) download(ctx context.Context) error {
	if err := os.MkdirAll(m.dataDir, 0755); err != nil {
		return fmt.Errorf("napcat mkdir: %w", err)
	}

	m.setupPhase.Store(SetupPhaseFetchInfo)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, githubReleaseAPI, nil)
	if err != nil {
		return fmt.Errorf("napcat: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := m.longClient.Do(req)
	if err != nil {
		return fmt.Errorf("napcat: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			Size               int64  `json:"size"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return fmt.Errorf("napcat parse release: %w", err)
	}

	wantAsset := shellAssetName()
	var downloadURL string
	var assetSize int64
	for _, a := range release.Assets {
		if a.Name == wantAsset {
			downloadURL = a.BrowserDownloadURL
			assetSize = a.Size
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("napcat: %s not found in release %s", wantAsset, release.TagName)
	}

	m.setupPhase.Store(SetupPhaseDownloading)
	m.setupTotal.Store(assetSize)
	m.setupProgress.Store(0)

	m.appendLog(fmt.Sprintf("downloading %s (%s)", wantAsset, release.TagName))

	dlReq, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return err
	}
	dlResp, err := m.longClient.Do(dlReq)
	if err != nil {
		return fmt.Errorf("napcat download: %w", err)
	}
	defer func() { _ = dlResp.Body.Close() }()

	if assetSize == 0 && dlResp.ContentLength > 0 {
		m.setupTotal.Store(dlResp.ContentLength)
	}

	tmpFile, err := os.CreateTemp(m.dataDir, "napcat-*.zip")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	pr := &progressReader{r: dlResp.Body, progress: &m.setupProgress}
	if _, err := io.Copy(tmpFile, pr); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("napcat save: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("napcat close temp file: %w", err)
	}

	m.setupPhase.Store(SetupPhaseExtracting)
	m.appendLog("extracting...")

	if err := os.RemoveAll(m.installDir); err != nil {
		return err
	}
	if err := os.MkdirAll(m.installDir, 0755); err != nil {
		return err
	}
	if err := unzip(tmpPath, m.installDir); err != nil {
		return fmt.Errorf("napcat unzip: %w", err)
	}

	m.mu.Lock()
	m.version = release.TagName
	m.mu.Unlock()

	m.appendLog(fmt.Sprintf("installed %s", release.TagName))
	return nil
}

// Start launches the NapCat Shell subprocess.
// NapCat handles wrapper.node resolution internally.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cmd != nil && m.cmd.Process != nil {
		return fmt.Errorf("napcat: already running")
	}

	// 清理上次残留的 NapCat 进程（应用非正常退出后遗留）
	m.cleanStaleProcess()

	if !m.isInstalled() {
		return fmt.Errorf("napcat: not installed")
	}

	nodeExe := m.resolveBundledNode()
	if nodeExe == "" {
		var err error
		nodeExe, err = FindNodeBinary()
		if err != nil {
			return fmt.Errorf("napcat: node not found")
		}
	}

	m.webuiPort = 0 // 从 stdout 解析实际端口
	m.wsPort = 6098
	m.httpServerPort = 3000
	m.webuiToken = randomHex(16)

	configDir := filepath.Join(m.installDir, "napcat", "config")

	// 从磁盘上已有的全局配置恢复 token，保持跨重启一致
	if loaded, err := LoadOneBotTokens(configDir); err == nil {
		if loaded.WSPort > 0 {
			m.wsPort = loaded.WSPort
		}
		m.wsToken = loaded.WSToken
		m.httpCallbackToken = loaded.CallbackToken
		if loaded.HTTPServerPort > 0 {
			m.httpServerPort = loaded.HTTPServerPort
		}
		m.httpServerToken = loaded.HTTPServerToken
	} else {
		m.wsToken = randomHex(16)
		m.httpCallbackToken = randomHex(16)
		m.httpServerToken = randomHex(16)
	}

	// 写入全局 OneBot 配置（onebot11.json），NapCat 启动时立即加载
	callbackURL := ""
	if m.apiPort > 0 {
		callbackURL = fmt.Sprintf("http://127.0.0.1:%d/v1/napcat/onebot-callback", m.apiPort)
	}
	obCfg := GenerateOneBotConfig(m.wsPort, m.wsToken, callbackURL, m.httpCallbackToken, m.httpServerPort, m.httpServerToken)
	if err := WriteOneBotConfig(configDir, obCfg); err != nil {
		return fmt.Errorf("napcat onebot config: %w", err)
	}

	if err := WriteWebUIConfig(configDir, 6099, m.webuiToken); err != nil {
		return fmt.Errorf("napcat webui config: %w", err)
	}

	// let NapCat resolve wrapper.node on its own
	sysEnv := os.Environ()
	env := make([]string, len(sysEnv), len(sysEnv)+1)
	copy(env, sysEnv)
	env = append(env, "NAPCAT_DISABLE_MULTI_PROCESS=1")

	entryScript := m.resolveEntryScript()
	if entryScript == "" {
		return fmt.Errorf("napcat: entry script not found in %s", m.installDir)
	}

	procCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel

	cmd := exec.CommandContext(procCtx, nodeExe, entryScript)
	cmd.Dir = m.installDir
	cmd.Env = env
	cmd.Stdout = m.newLogWriter()
	cmd.Stderr = m.newLogWriter()

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("napcat start: %w", err)
	}
	m.cmd = cmd
	m.savePID(cmd.Process.Pid)

	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		m.cmd = nil
		m.cancel = nil
		m.mu.Unlock()
		m.removePID()
		if err != nil && !strings.Contains(err.Error(), "signal: killed") {
			m.appendLog(fmt.Sprintf("process exited: %v", err))
		}
	}()

	m.appendLog(fmt.Sprintf("started (pid %d)", cmd.Process.Pid))

	// 获取 WebUI 鉴权凭证（启动后 WebUI 可能需要几秒就绪）
	go m.acquireCredential()

	return nil
}

// acquireCredential 通过 NapCat WebUI 的 /api/auth/login 获取签名凭证。
// 等待 stdout 中解析到实际端口后再尝试。
func (m *Manager) acquireCredential() {
	// 等待端口从 stdout 解析出来（最多 30 秒）
	var port int
	for i := 0; i < 60; i++ {
		time.Sleep(500 * time.Millisecond)
		m.mu.Lock()
		port = m.webuiPort
		m.mu.Unlock()
		if port > 0 {
			break
		}
	}
	if port == 0 {
		m.appendLog("webui credential acquisition failed: port not detected")
		return
	}

	m.mu.Lock()
	token := m.webuiToken
	m.mu.Unlock()

	hash := sha256Hex(token + ".napcat")
	loginURL := fmt.Sprintf("http://127.0.0.1:%d/api/auth/login", port)
	payload := map[string]string{"hash": hash}

	var credential string
	for i := 0; i < 10; i++ {
		time.Sleep(time.Duration(i+1) * 500 * time.Millisecond)
		body, err := m.webuiPost(loginURL, "", payload)
		if err != nil {
			continue
		}
		var resp struct {
			Code int `json:"code"`
			Data struct {
				Credential string `json:"Credential"`
			} `json:"data"`
		}
		if json.Unmarshal(body, &resp) != nil {
			continue
		}
		if resp.Code == 0 && resp.Data.Credential != "" {
			credential = resp.Data.Credential
			break
		}
	}

	m.mu.Lock()
	m.webuiCredential = credential
	m.mu.Unlock()

	if credential != "" {
		m.appendLog("webui credential acquired")
	} else {
		m.appendLog("webui credential acquisition failed")
	}
}

// refreshCredential 同步重新获取 WebUI credential（用于 credential 过期后恢复）。
func (m *Manager) refreshCredential() string {
	m.mu.Lock()
	port, token := m.webuiPort, m.webuiToken
	m.mu.Unlock()

	if port == 0 || token == "" {
		return ""
	}

	hash := sha256Hex(token + ".napcat")
	loginURL := fmt.Sprintf("http://127.0.0.1:%d/api/auth/login", port)
	payload := map[string]string{"hash": hash}

	body, err := m.webuiPost(loginURL, "", payload)
	if err != nil {
		return ""
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Credential string `json:"Credential"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &resp) != nil || resp.Code != 0 || resp.Data.Credential == "" {
		return ""
	}

	m.mu.Lock()
	m.webuiCredential = resp.Data.Credential
	m.mu.Unlock()
	return resp.Data.Credential
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		m.cmd = nil
	}
	m.removePID()
	m.appendLog("stopped")
	return nil
}

func (m *Manager) RefreshQRCode() error {
	m.mu.Lock()
	port, credential := m.webuiPort, m.webuiCredential
	m.mu.Unlock()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/QQLogin/RefreshQRcode", port)
	_, err := m.webuiPost(url, credential, nil)
	return err
}

func (m *Manager) WSEndpoint() (addr string, token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fmt.Sprintf("ws://127.0.0.1:%d", m.wsPort), m.wsToken
}

// OneBotHTTPEndpoint 返回 NapCat OneBot HTTP Server 地址和 token（用于发出站消息）
func (m *Manager) OneBotHTTPEndpoint() (addr string, token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fmt.Sprintf("http://127.0.0.1:%d", m.httpServerPort), m.httpServerToken
}

// HTTPCallbackToken 返回 NapCat HTTP Client 回调使用的 token
func (m *Manager) HTTPCallbackToken() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.httpCallbackToken
}

// VerifyCallbackSignature 校验 NapCat HTTP Client 的 OneBot11 HMAC-SHA1 签名。
// NapCat 用 config.token 作为 HMAC-SHA1 key 签名 request body，
// 签名放在 X-Signature header 中，格式为 "sha1=<hex>"。
// token 未配置时（空字符串）跳过校验。
func (m *Manager) VerifyCallbackSignature(body []byte, xSignature string) bool {
	m.mu.Lock()
	token := m.httpCallbackToken
	m.mu.Unlock()

	if token == "" {
		return true
	}

	if verifyHMACSHA1(token, body, xSignature) {
		return true
	}

	// 内存 token 可能过时，从磁盘 reload 重试
	configDir := filepath.Join(m.installDir, "napcat", "config")
	loaded, err := LoadOneBotTokens(configDir)
	if err != nil || loaded.CallbackToken == "" {
		return false
	}
	if loaded.CallbackToken == token {
		return false
	}
	if !verifyHMACSHA1(loaded.CallbackToken, body, xSignature) {
		return false
	}
	m.mu.Lock()
	m.httpCallbackToken = loaded.CallbackToken
	m.mu.Unlock()
	return true
}

func verifyHMACSHA1(key string, body []byte, xSignature string) bool {
	expected := strings.TrimPrefix(strings.TrimSpace(xSignature), "sha1=")
	if expected == "" {
		return false
	}
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write(body)
	actual := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(actual), []byte(expected))
}

// --- internal ---

// AutoStartIfReady 如果 NapCat 已安装，自动后台启动。
// NapCat 启动后由 Status() 检测登录状态并写入 OneBot 配置。
func (m *Manager) AutoStartIfReady() {
	m.mu.Lock()
	installed := m.isInstalled()
	running := m.cmd != nil && m.cmd.Process != nil
	m.mu.Unlock()

	if running || !installed {
		return
	}

	m.logger.Info("napcat: auto-starting")
	go func() {
		if err := m.Start(context.Background()); err != nil {
			m.logger.Warn("napcat: auto-start failed", "err", err)
		}
	}()
}

func (m *Manager) qrcodePNGPath() string {
	return filepath.Join(m.installDir, "napcat", "cache", "qrcode.png")
}

// QRCodeImagePath 返回本地 qrcode.png 的绝对路径
func (m *Manager) QRCodeImagePath() string {
	return m.qrcodePNGPath()
}

func (m *Manager) isInstalled() bool {
	return m.resolveEntryScript() != ""
}

func (m *Manager) resolveEntryScript() string {
	// index.js is the correct Shell entry -- it sets NAPCAT_WRAPPER_PATH,
	// NAPCAT_QQ_PACKAGE_INFO_PATH, NAPCAT_QQ_VERSION_CONFIG_PATH
	// before importing napcat/napcat.mjs.
	p := filepath.Join(m.installDir, "index.js")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

func (m *Manager) resolveBundledNode() string {
	bin := "node"
	if runtime.GOOS == "windows" {
		bin = "node.exe"
	}
	for _, c := range []string{
		filepath.Join(m.installDir, bin),
		filepath.Join(m.installDir, "node", bin),
	} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	matches, _ := filepath.Glob(filepath.Join(m.installDir, "node-v*", bin))
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

type loginStatusResponse struct {
	IsLogin    bool   `json:"isLogin"`
	IsOffline  bool   `json:"isOffline"`
	QRCodeURL  string `json:"qrcodeurl"`
	LoginError string `json:"loginError"`
}

func (m *Manager) checkLoginStatus() loginStatusResponse {
	m.mu.Lock()
	port, cred := m.webuiPort, m.webuiCredential
	m.mu.Unlock()

	if port == 0 {
		return loginStatusResponse{}
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/QQLogin/CheckLoginStatus", port)
	if result, ok := m.doCheckLoginStatus(url, cred); ok {
		return result
	}
	if cred = m.refreshCredential(); cred != "" {
		if result, ok := m.doCheckLoginStatus(url, cred); ok {
			return result
		}
	}
	return loginStatusResponse{}
}

func (m *Manager) doCheckLoginStatus(url, credential string) (loginStatusResponse, bool) {
	body, err := m.webuiPost(url, credential, nil)
	if err != nil {
		return loginStatusResponse{}, false
	}
	var resp struct {
		Code int                 `json:"code"`
		Data loginStatusResponse `json:"data"`
	}
	if json.Unmarshal(body, &resp) != nil || resp.Code != 0 {
		return loginStatusResponse{}, false
	}
	return resp.Data, true
}

type loginInfoResult struct {
	QQ       string `json:"uin"`
	Nickname string `json:"nick"`
}

func (m *Manager) getLoginInfo() loginInfoResult {
	m.mu.Lock()
	port, cred := m.webuiPort, m.webuiCredential
	m.mu.Unlock()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/QQLogin/GetQQLoginInfo", port)
	body, err := m.webuiPost(url, cred, nil)
	if err != nil {
		return loginInfoResult{}
	}
	var resp struct {
		Code int             `json:"code"`
		Data loginInfoResult `json:"data"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return loginInfoResult{}
	}
	return resp.Data
}

func (m *Manager) getQuickLoginList() []QuickLoginAccount {
	m.mu.Lock()
	port, cred := m.webuiPort, m.webuiCredential
	m.mu.Unlock()

	if port == 0 {
		return nil
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/QQLogin/GetQuickLoginListNew", port)
	body, err := m.webuiGetRaw(url, cred)
	if err != nil {
		return nil
	}
	var resp struct {
		Code int `json:"code"`
		Data []struct {
			Uin      string `json:"uin"`
			NickName string `json:"nickName"`
			FaceURL  string `json:"faceUrl"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &resp) != nil || resp.Code != 0 {
		return nil
	}
	var list []QuickLoginAccount
	for _, item := range resp.Data {
		if item.Uin == "" {
			continue
		}
		list = append(list, QuickLoginAccount{
			Uin:      item.Uin,
			Nickname: item.NickName,
			FaceURL:  item.FaceURL,
		})
	}
	return list
}

// IsLoggedIn 返回当前 QQ 是否处于登录状态。
func (m *Manager) IsLoggedIn() bool {
	m.mu.Lock()
	running := m.cmd != nil && m.cmd.Process != nil
	m.mu.Unlock()
	if !running {
		return false
	}
	login := m.checkLoginStatus()
	return login.IsLogin || login.IsOffline
}

// QuickLoginUins 返回可用于快速登录的 QQ 号列表。
func (m *Manager) QuickLoginUins() []string {
	list := m.getQuickLoginList()
	uins := make([]string, 0, len(list))
	for _, a := range list {
		if a.Uin != "" {
			uins = append(uins, a.Uin)
		}
	}
	return uins
}

// QuickLogin 使用已有账号快速登录
func (m *Manager) QuickLogin(uin string) error {
	m.mu.Lock()
	port, credential := m.webuiPort, m.webuiCredential
	m.mu.Unlock()

	if port == 0 {
		return fmt.Errorf("napcat: webui not ready")
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/api/QQLogin/SetQuickLogin", port)
	payload := map[string]string{"uin": uin}
	body, err := m.webuiPost(url, credential, payload)
	if err != nil {
		return fmt.Errorf("napcat quick login: %w", err)
	}
	var resp struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &resp) == nil && resp.Code != 0 {
		return fmt.Errorf("napcat quick login: %s", resp.Message)
	}
	return nil
}

func (m *Manager) webuiGetRaw(url, credential string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	resp, err := m.shortClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

func (m *Manager) webuiPost(url, credential string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = strings.NewReader(string(data))
	}
	req, err := http.NewRequest(http.MethodPost, url, reader)
	if err != nil {
		return nil, err
	}
	if credential != "" {
		req.Header.Set("Authorization", "Bearer "+credential)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := m.shortClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// --- log ring buffer ---

const maxLogLines = 200

func (m *Manager) appendLog(line string) {
	m.logMu.Lock()
	if len(m.logBuf) >= maxLogLines {
		m.logBuf = m.logBuf[1:]
	}
	m.logBuf = append(m.logBuf, line)
	m.logMu.Unlock()
}

// logWriter 同时写入日志缓冲区并扫描 NapCat stdout 提取实际 WebUI 端口
type logWriter struct {
	mgr *Manager
}

var webuiURLRe = regexp.MustCompile(`WebUi User Panel Url: https?://127\.0\.0\.1:(\d+)/`)

func (m *Manager) newLogWriter() *logWriter {
	return &logWriter{mgr: m}
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	lines := strings.Split(strings.TrimRight(string(p), "\n"), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		w.mgr.appendLog(line)
		// 从 NapCat stdout 提取实际 WebUI 端口
		if matches := webuiURLRe.FindStringSubmatch(line); len(matches) == 2 {
			if port, err := strconv.Atoi(matches[1]); err == nil && port > 0 {
				w.mgr.mu.Lock()
				if w.mgr.webuiPort == 0 {
					w.mgr.webuiPort = port
					w.mgr.logger.Info("napcat: detected webui port", "port", port)
				}
				w.mgr.mu.Unlock()
			}
		}
	}
	return len(p), nil
}

// --- progress reader ---

type progressReader struct {
	r        io.Reader
	progress *atomic.Int64
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.r.Read(p)
	if n > 0 {
		pr.progress.Add(int64(n))
	}
	return
}

// --- zip ---

func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(filepath.Clean(fpath), filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("invalid zip path: %s", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(fpath, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fpath), 0755); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			_ = outFile.Close()
			return err
		}
		_, err = io.Copy(outFile, rc)
		rcErr := rc.Close()
		outErr := outFile.Close()
		if err != nil {
			return err
		}
		if rcErr != nil {
			return rcErr
		}
		if outErr != nil {
			return outErr
		}
	}
	return nil
}

func sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
