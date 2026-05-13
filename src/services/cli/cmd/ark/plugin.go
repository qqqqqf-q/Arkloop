package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"arkloop/services/cli/internal/apiclient"
	"arkloop/services/cli/internal/formatter"
)

var errPluginUsage = errors.New("plugin usage")

func cmdPlugin(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printPluginUsage()
		return &exitError{2}
	}
	switch args[0] {
	case "list":
		return cmdPluginList(ctx, args[1:])
	case "install":
		return cmdPluginInstall(ctx, args[1:])
	case "uninstall":
		return cmdPluginUninstall(ctx, args[1:])
	case "enable":
		return cmdPluginEnablement(ctx, args[1:], true)
	case "disable":
		return cmdPluginEnablement(ctx, args[1:], false)
	case "info":
		return cmdPluginInfo(ctx, args[1:])
	case "settings":
		return cmdPluginSettings(ctx, args[1:])
	case "runtime":
		return cmdPluginRuntime(ctx, args[1:])
	case "search":
		return cmdPluginSearch(ctx, args[1:])
	default:
		printPluginUsage()
		return &exitError{2}
	}
}

func printPluginUsage() {
	fmt.Fprintln(os.Stderr, `usage: ark plugin <command> [flags]

commands:
  plugin list
  plugin install <source>
  plugin uninstall <plugin-id>
  plugin enable <plugin-id>
  plugin disable <plugin-id>
  plugin info <plugin-id>
  plugin settings <plugin-id> [key=value ...]
  plugin runtime status <plugin-id>
  plugin runtime install <plugin-id>
  plugin search [query]`)
}

func cmdPluginList(ctx context.Context, args []string) error {
	fs := newPluginFlagSet("plugin list")
	host, token, outputFormat := addAPIFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return pluginUsage("usage: ark plugin list [flags]")
	}
	if err := ensureOutputFormat(*outputFormat); err != nil {
		return err
	}
	items, err := newClientFromFlags(*host, *token, fs).ListPlugins(ctx)
	if err != nil {
		return err
	}
	return formatter.PrintPlugins(os.Stdout, *outputFormat, pluginPackageViews(items))
}

func cmdPluginInstall(ctx context.Context, args []string) error {
	fs := newPluginFlagSet("plugin install")
	host, token, outputFormat := addAPIFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return pluginUsage("usage: ark plugin install [flags] <source>")
	}
	if err := ensureOutputFormat(*outputFormat); err != nil {
		return err
	}
	req, err := pluginInstallRequest(fs.Arg(0))
	if err != nil {
		return err
	}
	item, err := newClientFromFlags(*host, *token, fs).InstallPlugin(ctx, req)
	if err != nil {
		return err
	}
	return formatter.PrintPlugin(os.Stdout, *outputFormat, pluginPackageView(item))
}

func cmdPluginUninstall(ctx context.Context, args []string) error {
	fs := newPluginFlagSet("plugin uninstall")
	host, token, outputFormat := addAPIFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return pluginUsage("usage: ark plugin uninstall [flags] <plugin-id>")
	}
	if err := ensureOutputFormat(*outputFormat); err != nil {
		return err
	}
	if err := newClientFromFlags(*host, *token, fs).UninstallPlugin(ctx, fs.Arg(0)); err != nil {
		return err
	}
	if *outputFormat == formatter.OutputJSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"ok": true, "plugin_id": fs.Arg(0)})
	}
	_, err := fmt.Fprintf(os.Stdout, "plugin_id: %s\nuninstalled: true\n", fs.Arg(0))
	return err
}

func cmdPluginEnablement(ctx context.Context, args []string, enabled bool) error {
	name := "plugin enable"
	if !enabled {
		name = "plugin disable"
	}
	fs := newPluginFlagSet(name)
	host, token, outputFormat := addAPIFlags(fs)
	workspaceRef := fs.String("workspace-ref", "", "workspace ref")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return pluginUsage("usage: ark " + name + " [flags] <plugin-id>")
	}
	if err := ensureOutputFormat(*outputFormat); err != nil {
		return err
	}
	item, err := newClientFromFlags(*host, *token, fs).SetPluginEnablement(ctx, fs.Arg(0), apiclient.PluginEnablementRequest{
		WorkspaceRef: *workspaceRef,
		Enabled:      enabled,
	})
	if err != nil {
		return err
	}
	return formatter.PrintPluginEnablement(os.Stdout, *outputFormat, pluginEnablementView(item))
}

func cmdPluginInfo(ctx context.Context, args []string) error {
	fs := newPluginFlagSet("plugin info")
	host, token, outputFormat := addAPIFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return pluginUsage("usage: ark plugin info [flags] <plugin-id>")
	}
	if err := ensureOutputFormat(*outputFormat); err != nil {
		return err
	}
	item, err := newClientFromFlags(*host, *token, fs).GetPlugin(ctx, fs.Arg(0))
	if err != nil {
		return err
	}
	return formatter.PrintPlugin(os.Stdout, *outputFormat, pluginPackageView(item))
}

func cmdPluginSettings(ctx context.Context, args []string) error {
	valueFlags := map[string]struct{}{"host": {}, "token": {}, "output-format": {}, "workspace-ref": {}}
	flagArgs, positionals, err := splitFlagAndPositionalArgs(args, valueFlags)
	if err != nil {
		return pluginUsage("usage: ark plugin settings [flags] <plugin-id> [key=value ...]")
	}

	fs := newPluginFlagSet("plugin settings")
	host, token, outputFormat := addAPIFlags(fs)
	workspaceRef := fs.String("workspace-ref", "", "workspace ref")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(positionals) < 1 {
		return pluginUsage("usage: ark plugin settings [flags] <plugin-id> [key=value ...]")
	}
	if err := ensureOutputFormat(*outputFormat); err != nil {
		return err
	}
	client := newClientFromFlags(*host, *token, fs)
	if len(positionals) == 1 {
		item, err := client.GetPlugin(ctx, positionals[0])
		if err != nil {
			return err
		}
		return formatter.PrintPlugin(os.Stdout, *outputFormat, pluginPackageView(item))
	}
	settings, err := parsePluginSettings(positionals[1:])
	if err != nil {
		return err
	}
	item, err := client.UpdatePluginSettings(ctx, positionals[0], apiclient.PluginSettingsRequest{
		WorkspaceRef: *workspaceRef,
		Settings:     settings,
	})
	if err != nil {
		return err
	}
	return formatter.PrintPluginEnablement(os.Stdout, *outputFormat, pluginEnablementView(item))
}

func cmdPluginRuntime(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return pluginUsage("usage: ark plugin runtime <status|install> [flags] <plugin-id>")
	}
	switch args[0] {
	case "status":
		return cmdPluginRuntimeStatus(ctx, args[1:])
	case "install":
		return cmdPluginRuntimeInstall(ctx, args[1:])
	default:
		return pluginUsage("usage: ark plugin runtime <status|install> [flags] <plugin-id>")
	}
}

func cmdPluginRuntimeStatus(ctx context.Context, args []string) error {
	fs := newPluginFlagSet("plugin runtime status")
	host, token, outputFormat := addAPIFlags(fs)
	workspaceRef := fs.String("workspace-ref", "", "workspace ref")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return pluginUsage("usage: ark plugin runtime status [flags] <plugin-id>")
	}
	if err := ensureOutputFormat(*outputFormat); err != nil {
		return err
	}
	status, err := newClientFromFlags(*host, *token, fs).GetPluginRuntimeStatus(ctx, fs.Arg(0), *workspaceRef)
	if err != nil {
		return err
	}
	return formatter.PrintPluginRuntime(os.Stdout, *outputFormat, pluginRuntimeView(status))
}

func cmdPluginRuntimeInstall(ctx context.Context, args []string) error {
	fs := newPluginFlagSet("plugin runtime install")
	host, token, outputFormat := addAPIFlags(fs)
	workspaceRef := fs.String("workspace-ref", "", "workspace ref")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return pluginUsage("usage: ark plugin runtime install [flags] <plugin-id>")
	}
	if err := ensureOutputFormat(*outputFormat); err != nil {
		return err
	}
	result, err := newClientFromFlags(*host, *token, fs).InstallPluginRuntime(ctx, fs.Arg(0), *workspaceRef)
	if err != nil {
		return err
	}
	return printAny(os.Stdout, *outputFormat, result)
}

func cmdPluginSearch(ctx context.Context, args []string) error {
	fs := newPluginFlagSet("plugin search")
	outputFormat := fs.String("output-format", formatter.OutputText, "output format: text, json")
	registryURL := fs.String("registry-url", "", "plugin registry URL")
	host := fs.String("host-requirement", "", "host requirement")
	platform := fs.String("platform", "", "platform")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return pluginUsage("usage: ark plugin search [flags] [query]")
	}
	if err := ensureOutputFormat(*outputFormat); err != nil {
		return err
	}
	resolvedURL := resolvePluginRegistryURL(*registryURL)
	if resolvedURL == "" {
		return fmt.Errorf("plugin registry URL is not configured")
	}
	query := ""
	if fs.NArg() == 1 {
		query = fs.Arg(0)
	}
	items, err := apiclient.SearchPluginRegistry(ctx, resolvedURL, query, *host, *platform)
	if err != nil {
		return err
	}
	return formatter.PrintPluginRegistry(os.Stdout, *outputFormat, pluginRegistryViews(items))
}

func newPluginFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func addAPIFlags(fs *flag.FlagSet) (*string, *string, *string) {
	host := fs.String("host", apiclient.DefaultBaseURL, "desktop API address")
	token := fs.String("token", "", "bearer token")
	outputFormat := fs.String("output-format", formatter.OutputText, "output format: text, json")
	return host, token, outputFormat
}

func pluginUsage(usage string) error {
	fmt.Fprintln(os.Stderr, usage)
	return &exitError{2}
}

func pluginInstallRequest(source string) (apiclient.PluginInstallRequest, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		return apiclient.PluginInstallRequest{}, errPluginUsage
	}
	if info, err := os.Stat(source); err == nil {
		_ = info
		abs, err := filepath.Abs(source)
		if err != nil {
			return apiclient.PluginInstallRequest{}, fmt.Errorf("resolve manifest path: %w", err)
		}
		return apiclient.PluginInstallRequest{ManifestPath: abs}, nil
	}
	if looksLikeURL(source) {
		return apiclient.PluginInstallRequest{SourceKind: "url", SourceURI: source}, nil
	}
	if looksLikePluginID(source) {
		return apiclient.PluginInstallRequest{SourceKind: "registry", SourceURI: source}, nil
	}
	if strings.ContainsAny(source, `/\`) || strings.HasPrefix(source, ".") {
		return apiclient.PluginInstallRequest{}, fmt.Errorf("plugin source path does not exist: %s", source)
	}
	return apiclient.PluginInstallRequest{}, fmt.Errorf("plugin source is invalid: %s", source)
}

func looksLikeURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}

func looksLikePluginID(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' || r == ':' {
			continue
		}
		return false
	}
	return true
}

func parsePluginSettings(pairs []string) (map[string]any, error) {
	settings := make(map[string]any, len(pairs))
	for _, pair := range pairs {
		key, raw, ok := strings.Cut(pair, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("settings must use key=value")
		}
		var value any
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			value = raw
		}
		settings[key] = value
	}
	return settings, nil
}

func resolvePluginRegistryURL(flagValue string) string {
	if strings.TrimSpace(flagValue) != "" {
		return strings.TrimSpace(flagValue)
	}
	if v := strings.TrimSpace(os.Getenv("ARKLOOP_PLUGIN_REGISTRY_URL")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(home, ".arkloop", "config.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		PluginRegistryURL string `json:"plugin_registry_url"`
		PluginRegistryUrl string `json:"pluginRegistryUrl"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ""
	}
	if strings.TrimSpace(cfg.PluginRegistryURL) != "" {
		return strings.TrimSpace(cfg.PluginRegistryURL)
	}
	return strings.TrimSpace(cfg.PluginRegistryUrl)
}

func pluginPackageViews(items []apiclient.PluginPackage) []formatter.PluginView {
	views := make([]formatter.PluginView, 0, len(items))
	for _, item := range items {
		views = append(views, pluginPackageView(item))
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].ID != views[j].ID {
			return views[i].ID < views[j].ID
		}
		return views[i].Version < views[j].Version
	})
	return views
}

func pluginPackageView(item apiclient.PluginPackage) formatter.PluginView {
	return formatter.PluginView{
		ID:          item.ID,
		Version:     item.Version,
		DisplayName: item.DisplayName,
		Description: item.Description,
		SourceKind:  item.SourceKind,
		SourceURI:   item.SourceURI,
		IsActive:    item.IsActive,
		Manifest:    item.Manifest,
	}
}

func pluginEnablementView(item apiclient.PluginEnablement) formatter.PluginEnablementView {
	return formatter.PluginEnablementView{
		PluginID:     item.PluginID,
		Version:      item.PluginVersion,
		WorkspaceRef: item.WorkspaceRef,
		Enabled:      item.Enabled,
		Settings:     item.SettingsJSON,
		UpdatedAt:    item.UpdatedAt,
	}
}

func pluginRuntimeView(status apiclient.PluginRuntimeStatus) formatter.PluginRuntimeView {
	return formatter.PluginRuntimeView{
		PluginID:     status.PluginID,
		Version:      status.PluginVersion,
		WorkspaceRef: status.WorkspaceRef,
		Status:       firstNonEmptyString(status.Status, "not_installed"),
		Details:      status.StatusJSON,
		UpdatedAt:    status.UpdatedAt,
	}
}

func pluginRegistryViews(items []apiclient.PluginRegistryItem) []formatter.PluginRegistryView {
	views := make([]formatter.PluginRegistryView, 0, len(items))
	for _, item := range items {
		views = append(views, formatter.PluginRegistryView{
			ID:              item.ID,
			Name:            item.Name,
			Publisher:       item.Publisher,
			Description:     item.Description,
			HostRequirement: item.HostRequirement,
			Platforms:       append([]string{}, item.Platforms...),
			LatestVersion:   item.LatestVersion,
		})
	}
	return views
}

func printAny(w io.Writer, outputFormat string, value any) error {
	switch outputFormat {
	case formatter.OutputText:
		return json.NewEncoder(w).Encode(value)
	case formatter.OutputJSON:
		return json.NewEncoder(w).Encode(value)
	default:
		return fmt.Errorf("unknown output format: %s", outputFormat)
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
