package napcat

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// OneBotNetworkConfig 是 NapCat 的 OneBot11 网络配置
type OneBotNetworkConfig struct {
	HTTPServers      []HTTPServerConfig `json:"httpServers"`
	HTTPSseServers   []json.RawMessage  `json:"httpSseServers"`
	HTTPClients      []HTTPClientConfig `json:"httpClients"`
	WebsocketServers []WSServerConfig   `json:"websocketServers"`
	WebsocketClients []json.RawMessage  `json:"websocketClients"`
	Plugins          []json.RawMessage  `json:"plugins"`
}

type WSServerConfig struct {
	Name                 string `json:"name"`
	Enable               bool   `json:"enable"`
	Host                 string `json:"host"`
	Port                 int    `json:"port"`
	MessagePostFormat    string `json:"messagePostFormat"`
	ReportSelfMessage    bool   `json:"reportSelfMessage"`
	Token                string `json:"token"`
	EnableForcePushEvent bool   `json:"enableForcePushEvent"`
	Debug                bool   `json:"debug"`
	HeartInterval        int    `json:"heartInterval"`
}

type HTTPClientConfig struct {
	Name              string `json:"name"`
	Enable            bool   `json:"enable"`
	URL               string `json:"url"`
	MessagePostFormat string `json:"messagePostFormat"`
	ReportSelfMessage bool   `json:"reportSelfMessage"`
	Token             string `json:"token"`
	Debug             bool   `json:"debug"`
}

type HTTPServerConfig struct {
	Name              string `json:"name"`
	Enable            bool   `json:"enable"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	MessagePostFormat string `json:"messagePostFormat"`
	ReportSelfMessage bool   `json:"reportSelfMessage"`
	Token             string `json:"token"`
	Debug             bool   `json:"debug"`
}

type OneBotFullConfig struct {
	Network         OneBotNetworkConfig `json:"network"`
	MusicSignURL    string              `json:"musicSignUrl"`
	EnableLocalFile bool                `json:"enableLocalFile2Url"`
	ParseMultMsg    bool                `json:"parseMultMsg"`
}

// GenerateOneBotConfig 为 Arkloop 生成 NapCat OneBot11 配置
// wsPort: WS Server 监听端口，wsToken: WS 鉴权 token
// httpCallbackURL: NapCat 主动 POST 事件的目标地址（HTTP Client 模式）
// httpCallbackToken: 回调鉴权 token
// httpServerPort: OneBot HTTP Server 端口（Arkloop 通过它发出站消息）
// httpServerToken: HTTP Server 鉴权 token
func GenerateOneBotConfig(wsPort int, wsToken string, httpCallbackURL, httpCallbackToken string, httpServerPort int, httpServerToken string) OneBotFullConfig {
	cfg := OneBotFullConfig{
		Network: OneBotNetworkConfig{
			HTTPServers:    []HTTPServerConfig{},
			HTTPSseServers: []json.RawMessage{},
			HTTPClients:    []HTTPClientConfig{},
			WebsocketServers: []WSServerConfig{{
				Name:                 "arkloop-ws",
				Enable:               true,
				Host:                 "127.0.0.1",
				Port:                 wsPort,
				MessagePostFormat:    "array",
				ReportSelfMessage:    false,
				Token:                wsToken,
				EnableForcePushEvent: true,
				Debug:                false,
				HeartInterval:        30000,
			}},
			WebsocketClients: []json.RawMessage{},
			Plugins:          []json.RawMessage{},
		},
		EnableLocalFile: true,
		ParseMultMsg:    true,
	}
	if httpCallbackURL != "" {
		cfg.Network.HTTPClients = append(cfg.Network.HTTPClients, HTTPClientConfig{
			Name:              "arkloop-callback",
			Enable:            true,
			URL:               httpCallbackURL,
			MessagePostFormat: "array",
			ReportSelfMessage: false,
			Token:             httpCallbackToken,
			Debug:             false,
		})
	}
	if httpServerPort > 0 {
		cfg.Network.HTTPServers = append(cfg.Network.HTTPServers, HTTPServerConfig{
			Name:              "arkloop-http",
			Enable:            true,
			Host:              "127.0.0.1",
			Port:              httpServerPort,
			MessagePostFormat: "array",
			ReportSelfMessage: false,
			Token:             httpServerToken,
			Debug:             false,
		})
	}
	return cfg
}

// WriteOneBotConfig 将 OneBot 配置写入 NapCat 的 config 目录。
// 写入 onebot11.json 全局默认配置，同时同步到所有已存在的 onebot11_{uin}.json，
// 因为 NapCat 会优先读取账号特定配置。
func WriteOneBotConfig(configDir string, cfg OneBotFullConfig) error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("mkdir config: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal onebot config: %w", err)
	}
	// 全局默认配置
	if err := os.WriteFile(filepath.Join(configDir, "onebot11.json"), data, 0644); err != nil {
		return err
	}
	// 同步到所有已存在的 onebot11_{uin}.json
	matches, _ := filepath.Glob(filepath.Join(configDir, "onebot11_*.json"))
	for _, path := range matches {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// NapCatWebUIConfig 是 NapCat WebUI 配置
type NapCatWebUIConfig struct {
	Host      string `json:"host"`
	Port      int    `json:"port"`
	Token     string `json:"token"`
	LoginRate int    `json:"loginRate"`
}

// WriteWebUIConfig 写入 WebUI 配置
func WriteWebUIConfig(configDir string, port int, token string) error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("mkdir config: %w", err)
	}
	cfg := NapCatWebUIConfig{
		Host:      "127.0.0.1",
		Port:      port,
		Token:     token,
		LoginRate: 3,
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal webui config: %w", err)
	}
	return os.WriteFile(filepath.Join(configDir, "webui.json"), data, 0644)
}

// LoadOneBotConfig 从磁盘读取已有的 onebot11_*.json 配置
type LoadedOneBotTokens struct {
	WSPort          int
	WSToken         string
	HTTPServerPort  int
	HTTPServerToken string
	CallbackToken   string
}

func LoadOneBotTokens(configDir string) (*LoadedOneBotTokens, error) {
	// 优先读取全局默认配置
	candidates := []string{filepath.Join(configDir, "onebot11.json")}
	// 兼容旧的 uin 命名配置
	if matches, _ := filepath.Glob(filepath.Join(configDir, "onebot11_*.json")); len(matches) > 0 {
		candidates = append(candidates, matches...)
	}
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var cfg OneBotFullConfig
		if err := json.Unmarshal(data, &cfg); err != nil {
			continue
		}
		if len(cfg.Network.WebsocketServers) == 0 {
			continue
		}
		t := &LoadedOneBotTokens{}
		t.WSPort = cfg.Network.WebsocketServers[0].Port
		t.WSToken = cfg.Network.WebsocketServers[0].Token
		if len(cfg.Network.HTTPServers) > 0 {
			t.HTTPServerPort = cfg.Network.HTTPServers[0].Port
			t.HTTPServerToken = cfg.Network.HTTPServers[0].Token
		}
		if len(cfg.Network.HTTPClients) > 0 {
			t.CallbackToken = cfg.Network.HTTPClients[0].Token
		}
		return t, nil
	}
	return nil, fmt.Errorf("no valid onebot config found")
}
