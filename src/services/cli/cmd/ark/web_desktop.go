//go:build desktop

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
	"unicode"

	"arkloop/services/desktop/runtime"
	"arkloop/services/shared/database/sqliteadapter"
	"arkloop/services/shared/database/sqlitepgx"
	"arkloop/services/shared/desktop"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/term"
)

const (
	defaultWebHost    = "127.0.0.1"
	defaultWebPort    = 19080
	defaultAPIPort    = 19001
	defaultBridgePort = 19003

	desktopUserID    = "00000000-0000-4000-8000-000000000001"
	desktopAccountID = "00000000-0000-4000-8000-000000000002"
	desktopRole      = "platform_admin"
)

type localTrustConfig struct {
	Enabled bool
}

func cmdWeb(ctx context.Context, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "reset-password", "set-password", "init-admin":
			return cmdWebResetPassword(ctx, args[1:])
		}
	}
	return cmdWebStart(ctx, args)
}

func cmdWebStart(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("web", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	host := fs.String("host", defaultWebHost, "web listen host")
	port := fs.Int("port", defaultWebPort, "web listen port")
	apiPort := fs.Int("api-port", defaultAPIPort, "local api port")
	bridgePort := fs.Int("bridge-port", defaultBridgePort, "local bridge port")
	webRoot := fs.String("web-root", "", "web dist directory")
	dataDir := fs.String("data-dir", "", "data directory")
	publicURL := fs.String("public-url", "", "public base URL")
	noOpen := fs.Bool("no-open", false, "do not open browser")
	verbose := fs.Bool("verbose", false, "show runtime logs")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		printWebUsage()
		return &exitError{2}
	}

	root, err := resolveWebRoot(*webRoot)
	if err != nil {
		return err
	}

	if err := configureHeadlessEnv(*apiPort, *bridgePort, *dataDir, *publicURL); err != nil {
		return err
	}
	if err := desktopruntime.EnsureToken(); err != nil {
		return err
	}
	if err := writeDesktopPort(*apiPort); err != nil {
		return err
	}
	ownerPasswordSet, err := desktopOwnerPasswordCredentialExists(ctx, *dataDir)
	if err != nil {
		return err
	}

	runtimeErr := make(chan error, 1)
	go func() {
		runtimeErr <- desktopruntime.Run(ctx, desktopruntime.Options{
			Component:    "headless-web",
			StartBridge:  true,
			StartSandbox: true,
			Quiet:        !*verbose,
		})
	}()

	waitCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	select {
	case err := <-runtimeErr:
		return err
	case err := <-waitAPIReady(waitCtx):
		if err != nil {
			return err
		}
	}

	localTrust, err := localTrustConfigForHost(*host)
	if err != nil {
		return err
	}

	apiURL := fmt.Sprintf("http://127.0.0.1:%d", *apiPort)
	webAddr := net.JoinHostPort(strings.TrimSpace(*host), strconv.Itoa(*port))
	server := newHeadlessWebServer(webAddr, root, apiURL, localTrust)
	listener, err := net.Listen("tcp", webAddr)
	if err != nil {
		return err
	}
	webErr := make(chan error, 1)
	go func() {
		webErr <- server.Serve(listener)
	}()

	localURL := "http://" + net.JoinHostPort(localDisplayHost(*host), listenPort(listener.Addr(), *port))
	webURL := localURL
	openURL := webURL
	if localTrust.Enabled && !ownerPasswordSet {
		setupURL := localURL + "/setup"
		openURL = setupURL
	}
	printHeadlessWebPanel(os.Stdout, headlessWebPanel{
		WebURL:      webURL,
		SetupURL:    setupURLForPanel(localURL, localTrust.Enabled && !ownerPasswordSet),
		APIURL:      apiURL,
		WebRoot:     root,
		ListenAddr:  webAddr,
		OpenedURL:   openURL,
		RemoteLogin: ownerPasswordSet,
	})
	if !*noOpen {
		if err := openExternalBrowser(openURL); err != nil {
			fmt.Fprintf(os.Stderr, "open_error=%s\n", err)
		}
	}

	select {
	case err := <-runtimeErr:
		_ = shutdownWebServer(server)
		return err
	case err := <-webErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		return shutdownWebServer(server)
	}
}

func printWebUsage() {
	fmt.Fprintln(os.Stderr, "usage: ark web [flags]")
	fmt.Fprintln(os.Stderr, "       ark web reset-password [flags]")
}

type headlessWebPanel struct {
	WebURL      string
	SetupURL    string
	APIURL      string
	WebRoot     string
	ListenAddr  string
	OpenedURL   string
	RemoteLogin bool
}

func setupURLForPanel(localURL string, required bool) string {
	if !required {
		return ""
	}
	return localURL + "/setup"
}

func printHeadlessWebPanel(output *os.File, panel headlessWebPanel) {
	if output == nil {
		return
	}
	if !term.IsTerminal(int(output.Fd())) {
		printHeadlessWebKV(output, panel)
		return
	}
	fmt.Fprint(output, "\033[2J\033[H")
	fmt.Fprintln(output, "\033[1;38;5;75mARKLOOP WEB\033[0m")
	fmt.Fprintln(output)
	if panel.SetupURL != "" {
		fmt.Fprintf(output, "  \033[1;33mSetup:\033[0m          \033[1m%s\033[0m\n", panel.SetupURL)
	}
	fmt.Fprintf(output, "  \033[1;36mWeb interface:\033[0m  \033[1m%s\033[0m\n", panel.WebURL)
	fmt.Fprintf(output, "  \033[90mAPI:\033[0m            %s\n", panel.APIURL)
	fmt.Fprintf(output, "  \033[90mListening:\033[0m      %s\n", panel.ListenAddr)
	if panel.RemoteLogin {
		fmt.Fprintln(output, "  \033[90mRemote:\033[0m         password login enabled")
	} else {
		fmt.Fprintln(output, "  \033[90mRemote:\033[0m         setup required")
	}
	fmt.Fprintln(output)
	fmt.Fprintf(output, "  \033[90mOpened:\033[0m         %s\n", panel.OpenedURL)
	fmt.Fprintln(output, "  \033[90mStop:\033[0m           Ctrl+C")
	fmt.Fprintln(output)
}

func printHeadlessWebKV(output io.Writer, panel headlessWebPanel) {
	if panel.SetupURL != "" {
		fmt.Fprintf(output, "setup_url=%s\n", panel.SetupURL)
	}
	fmt.Fprintf(output, "web_url=%s\n", panel.WebURL)
	fmt.Fprintf(output, "api_url=%s\n", panel.APIURL)
	fmt.Fprintf(output, "web_root=%s\n", panel.WebRoot)
	fmt.Fprintf(output, "opened_url=%s\n", panel.OpenedURL)
}

func configureHeadlessEnv(apiPort int, bridgePort int, dataDir string, publicURL string) error {
	if apiPort <= 0 || apiPort > 65535 {
		return fmt.Errorf("api-port must be in range 1-65535")
	}
	if bridgePort <= 0 || bridgePort > 65535 {
		return fmt.Errorf("bridge-port must be in range 1-65535")
	}
	if err := os.Setenv("ARKLOOP_API_GO_ADDR", net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort))); err != nil {
		return err
	}
	if err := os.Setenv("ARKLOOP_BRIDGE_ADDR", net.JoinHostPort("127.0.0.1", strconv.Itoa(bridgePort))); err != nil {
		return err
	}
	if strings.TrimSpace(dataDir) != "" {
		if err := os.Setenv("ARKLOOP_DATA_DIR", strings.TrimSpace(dataDir)); err != nil {
			return err
		}
	}
	if strings.TrimSpace(publicURL) != "" {
		if err := os.Setenv("ARKLOOP_APP_BASE_URL", strings.TrimRight(strings.TrimSpace(publicURL), "/")); err != nil {
			return err
		}
	}
	return nil
}

func waitAPIReady(ctx context.Context) <-chan error {
	ch := make(chan error, 1)
	go func() {
		ch <- desktop.WaitAPIReady(ctx)
	}()
	return ch
}

func localDisplayHost(host string) string {
	switch strings.TrimSpace(host) {
	case "", "0.0.0.0", "::":
		return "127.0.0.1"
	default:
		return strings.TrimSpace(host)
	}
}

func listenPort(addr net.Addr, fallback int) string {
	if tcpAddr, ok := addr.(*net.TCPAddr); ok && tcpAddr.Port > 0 {
		return strconv.Itoa(tcpAddr.Port)
	}
	return strconv.Itoa(fallback)
}

func writeDesktopPort(port int) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	path := filepath.Join(home, ".arkloop", "desktop.port")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(port)), 0o600)
}

func shutdownWebServer(server *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		return server.Close()
	}
	return nil
}

func newHeadlessWebServer(addr string, webRoot string, apiURL string, localTrust localTrustConfig) *http.Server {
	target, _ := url.Parse(apiURL)
	proxy := httputil.NewSingleHostReverseProxy(target)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/") || r.URL.Path == "/v1" {
			proxy.ServeHTTP(w, r)
			return
		}
		serveWebAsset(w, r, webRoot, localTrust)
	})
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
}

func serveWebAsset(w http.ResponseWriter, r *http.Request, webRoot string, localTrust localTrustConfig) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	assetPath, ok := resolveAssetPath(webRoot, r.URL.Path)
	if !ok {
		serveIndex(w, r, webRoot, localTrust)
		return
	}
	if info, err := os.Stat(assetPath); err == nil && !info.IsDir() {
		if !assetPathWithinRoot(webRoot, assetPath) {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, assetPath)
		return
	}
	serveIndex(w, r, webRoot, localTrust)
}

func resolveAssetPath(webRoot string, requestPath string) (string, bool) {
	normalized := strings.ReplaceAll(requestPath, "\\", "/")
	cleanPath := path.Clean("/" + strings.TrimPrefix(normalized, "/"))
	if cleanPath == "/" {
		return "", false
	}
	relPath := strings.TrimPrefix(cleanPath, "/")
	if relPath == "" {
		return "", false
	}
	root, err := filepath.Abs(webRoot)
	if err != nil {
		root = webRoot
	}
	candidate := filepath.Join(root, filepath.FromSlash(relPath))
	if !pathWithinRoot(root, candidate) {
		return "", false
	}
	return candidate, true
}

func assetPathWithinRoot(webRoot string, assetPath string) bool {
	root, err := filepath.Abs(webRoot)
	if err != nil {
		return false
	}
	asset, err := filepath.Abs(assetPath)
	if err != nil {
		return false
	}
	if !pathWithinRoot(root, asset) {
		return false
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return false
	}
	realAsset, err := filepath.EvalSymlinks(asset)
	if err != nil {
		return false
	}
	return pathWithinRoot(realRoot, realAsset)
}

func pathWithinRoot(root string, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func serveIndex(w http.ResponseWriter, r *http.Request, webRoot string, localTrust localTrustConfig) {
	indexPath := filepath.Join(webRoot, "index.html")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		http.Error(w, "web assets not found", http.StatusInternalServerError)
		return
	}
	if localTrustAllowed(w, r, localTrust) {
		data = injectDesktopInfo(data, strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_TOKEN")))
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

func localTrustConfigForHost(host string) (localTrustConfig, error) {
	switch requestHostName(host) {
	case "localhost", "127.0.0.1", "::1", "", "0.0.0.0", "::":
		return localTrustConfig{Enabled: true}, nil
	default:
		return localTrustConfig{}, nil
	}
}

func localTrustAllowed(w http.ResponseWriter, r *http.Request, cfg localTrustConfig) bool {
	if !cfg.Enabled || !isLocalWebRequest(r) {
		return false
	}
	return true
}

func injectDesktopInfo(index []byte, token string) []byte {
	if strings.TrimSpace(token) == "" {
		return index
	}
	payload, _ := json.Marshal(map[string]string{
		"accessToken":   token,
		"bridgeBaseUrl": "",
		"mode":          "local",
	})
	script := []byte(`<script>window.__ARKLOOP_DESKTOP__=Object.assign(` + string(payload) + `,{getApiBaseUrl:function(){return window.location.origin},getBridgeBaseUrl:function(){return ""},getAccessToken:function(){return ` + strconv.Quote(token) + `},getMode:function(){return "local"}});</script>`)
	if idx := bytes.Index(index, []byte("</head>")); idx >= 0 {
		out := make([]byte, 0, len(index)+len(script))
		out = append(out, index[:idx]...)
		out = append(out, script...)
		out = append(out, index[idx:]...)
		return out
	}
	return append(script, index...)
}

func isLocalWebRequest(r *http.Request) bool {
	host := requestHostName(r.Host)
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return false
	}
	if !localForwardedHeadersAllowed(r.Header) {
		return false
	}
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}
	ip := net.ParseIP(strings.TrimSpace(remoteHost))
	return ip != nil && ip.IsLoopback()
}

func localForwardedHeadersAllowed(header http.Header) bool {
	if !forwardedHostValuesAllowed(header.Values("X-Forwarded-Host")) {
		return false
	}
	if !forwardedIPValuesAllowed(header.Values("X-Forwarded-For")) {
		return false
	}
	if !forwardedIPValuesAllowed(header.Values("X-Real-IP")) {
		return false
	}
	for _, value := range header.Values("Forwarded") {
		for _, part := range strings.Split(value, ",") {
			for _, param := range strings.Split(part, ";") {
				key, raw, ok := strings.Cut(param, "=")
				if !ok {
					continue
				}
				switch strings.ToLower(strings.TrimSpace(key)) {
				case "host":
					if !forwardedHostAllowed(raw) {
						return false
					}
				case "for":
					if !forwardedIPAllowed(raw) {
						return false
					}
				}
			}
		}
	}
	return true
}

func forwardedHostValuesAllowed(values []string) bool {
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			if !forwardedHostAllowed(raw) {
				return false
			}
		}
	}
	return true
}

func forwardedHostAllowed(raw string) bool {
	host := requestHostName(trimForwardedHeaderValue(raw))
	if host == "" {
		return true
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func forwardedIPValuesAllowed(values []string) bool {
	for _, value := range values {
		for _, raw := range strings.Split(value, ",") {
			if !forwardedIPAllowed(raw) {
				return false
			}
		}
	}
	return true
}

func forwardedIPAllowed(raw string) bool {
	host := trimForwardedHeaderValue(raw)
	if host == "" {
		return true
	}
	if strings.EqualFold(host, "unknown") {
		return false
	}
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func trimForwardedHeaderValue(raw string) string {
	return strings.Trim(strings.TrimSpace(raw), `"`)
}

func requestHostName(hostport string) string {
	host := strings.TrimSpace(hostport)
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	return strings.ToLower(host)
}

func resolveWebRoot(explicit string) (string, error) {
	candidates := []string{}
	if strings.TrimSpace(explicit) != "" {
		candidates = append(candidates, strings.TrimSpace(explicit))
	}
	if strings.TrimSpace(webRootHint) != "" {
		candidates = append(candidates, strings.TrimSpace(webRootHint))
	}
	if env := strings.TrimSpace(os.Getenv("ARKLOOP_WEB_ROOT")); env != "" {
		candidates = append(candidates, env)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "src", "apps", "web", "dist"),
			filepath.Join(cwd, "web", "dist"),
		)
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "web"),
			filepath.Join(dir, "web", "dist"),
			filepath.Join(dir, "..", "web"),
			filepath.Join(dir, "..", "src", "apps", "web", "dist"),
		)
	}
	for _, candidate := range candidates {
		if okWebRoot(candidate) {
			abs, err := filepath.Abs(candidate)
			if err != nil {
				return candidate, nil
			}
			return abs, nil
		}
	}
	return "", fmt.Errorf("web assets not found")
}

func okWebRoot(path string) bool {
	info, err := os.Stat(filepath.Join(path, "index.html"))
	return err == nil && !info.IsDir()
}

func cmdWebResetPassword(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("web reset-password", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	username := fs.String("username", "", "login username")
	password := fs.String("password", "", "login password")
	dataDir := fs.String("data-dir", "", "data directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		printWebUsage()
		return &exitError{2}
	}

	login := strings.TrimSpace(*username)
	if login == "" {
		login = desktopPreferredUsername()
	}
	pass := *password
	if pass == "" {
		var err error
		pass, err = readPasswordTwice(os.Stdin, os.Stderr)
		if err != nil {
			return err
		}
	}
	if err := validateOwnerPassword(pass); err != nil {
		return err
	}
	if err := setDesktopOwnerPassword(ctx, *dataDir, login, pass); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "username=%s\n", login)
	fmt.Fprintln(os.Stdout, "password_set=true")
	return nil
}

func readPasswordTwice(stdin *os.File, output io.Writer) (string, error) {
	if !term.IsTerminal(int(stdin.Fd())) {
		return "", fmt.Errorf("password is required")
	}
	fmt.Fprint(output, "password: ")
	first, err := term.ReadPassword(int(stdin.Fd()))
	fmt.Fprintln(output)
	if err != nil {
		return "", err
	}
	fmt.Fprint(output, "confirm_password: ")
	second, err := term.ReadPassword(int(stdin.Fd()))
	fmt.Fprintln(output)
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", fmt.Errorf("password confirmation does not match")
	}
	return string(first), nil
}

func desktopOwnerPasswordCredentialExists(ctx context.Context, explicitDataDir string) (bool, error) {
	dataDir, err := desktop.ResolveDataDir(explicitDataDir)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return false, err
	}
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(dataDir, "data.db"))
	if err != nil {
		return false, err
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(1) FROM user_credentials WHERE user_id = $1`, desktopUserID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func setDesktopOwnerPassword(ctx context.Context, explicitDataDir string, login string, password string) error {
	dataDir, err := desktop.ResolveDataDir(explicitDataDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	sqlitePool, err := sqliteadapter.AutoMigrate(ctx, filepath.Join(dataDir, "data.db"))
	if err != nil {
		return err
	}
	defer sqlitePool.Close()

	pool := sqlitepgx.New(sqlitePool.Unwrap())
	if err := seedDesktopOwner(ctx, pool); err != nil {
		return err
	}
	rawHash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return err
	}
	hash := string(rawHash)

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `UPDATE users SET username = $1 WHERE id = $2`, login, desktopUserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_credentials WHERE user_id = $1`, desktopUserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_credentials (user_id, login, password_hash) VALUES ($1, $2, $3)`, desktopUserID, login, hash); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func openExternalBrowser(targetURL string) error {
	targetURL = strings.TrimSpace(targetURL)
	if targetURL == "" {
		return nil
	}

	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "darwin":
		cmd = exec.Command("open", targetURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", targetURL)
	default:
		cmd = exec.Command("xdg-open", targetURL)
	}
	return cmd.Start()
}

func seedDesktopOwner(ctx context.Context, q *sqlitepgx.Pool) error {
	username := desktopPreferredUsername()
	if _, err := q.Exec(ctx, `
		INSERT INTO users (id, username, email, status)
		VALUES ($1, $2, 'desktop@localhost', 'active')
		ON CONFLICT (id) DO NOTHING`,
		desktopUserID, username,
	); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO accounts (id, slug, name, type, owner_user_id)
		VALUES ($1, 'desktop', 'Desktop', 'personal', $2)
		ON CONFLICT (id) DO NOTHING`,
		desktopAccountID, desktopUserID,
	); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO account_memberships (account_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (account_id, user_id) DO NOTHING`,
		desktopAccountID, desktopUserID, desktopRole,
	); err != nil {
		return err
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO credits (account_id, balance)
		VALUES ($1, 999999999)
		ON CONFLICT (account_id) DO NOTHING`,
		desktopAccountID,
	); err != nil {
		return err
	}
	return nil
}

func desktopPreferredUsername() string {
	if v := strings.TrimSpace(os.Getenv("ARKLOOP_DESKTOP_OS_USERNAME")); v != "" {
		return v
	}
	return "desktop"
}

func validateOwnerPassword(password string) error {
	if len(password) < 8 || len(password) > 72 {
		return fmt.Errorf("password must be 8-72 characters and include letters and numbers")
	}
	hasLetter := false
	hasDigit := false
	for _, char := range password {
		if unicode.IsLetter(char) {
			hasLetter = true
		}
		if unicode.IsDigit(char) {
			hasDigit = true
		}
		if hasLetter && hasDigit {
			return nil
		}
	}
	return fmt.Errorf("password must be 8-72 characters and include letters and numbers")
}
