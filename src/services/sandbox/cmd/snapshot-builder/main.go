//go:build !desktop

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"arkloop/services/sandbox/internal/app"
	"arkloop/services/sandbox/internal/firecracker"
	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/snapshot"
	"arkloop/services/sandbox/internal/storage"
	"arkloop/services/sandbox/internal/template"
	"arkloop/services/shared/objectstore"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printUsage()
		return nil
	}

	if _, err := app.LoadDotenvIfEnabled(false); err != nil {
		return fmt.Errorf("load dotenv: %w", err)
	}

	cfg, err := app.LoadConfigFromEnv()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	subcommand := args[0]
	rest := args[1:]

	switch subcommand {
	case "create":
		return runCreate(cfg, rest)
	case "list":
		return runList(cfg)
	default:
		printUsage()
		return fmt.Errorf("unknown subcommand %q", subcommand)
	}
}

func runCreate(cfg app.Config, args []string) error {
	fs := flag.NewFlagSet("create", flag.ContinueOnError)
	templateID := fs.String("template", "", "template ID to build (e.g. python3.12-lite or chromium-browser)")
	all := fs.Bool("all", false, "build snapshots for all templates that are missing")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *templateID == "" && !*all {
		return fmt.Errorf("specify --template <id> or --all")
	}

	store, registry, logger, err := initDeps(cfg)
	if err != nil {
		return err
	}
	networkManager, err := initNetworkManager(cfg)
	if err != nil {
		return err
	}

	builder := snapshot.NewBuilder(
		cfg.FirecrackerBin,
		cfg.SocketBaseDir,
		cfg.BootTimeoutSeconds,
		cfg.GuestAgentPort,
		store,
		networkManager,
		logger,
	)

	ctx := context.Background()

	if *all {
		return builder.EnsureAll(ctx, registry)
	}

	tmpl, ok := registry.Get(strings.TrimSpace(*templateID))
	if !ok {
		return fmt.Errorf("template %q not found in registry", *templateID)
	}
	return builder.Build(ctx, tmpl)
}

func runList(cfg app.Config) error {
	store, registry, _, err := initDeps(cfg)
	if err != nil {
		return err
	}

	ctx := context.Background()
	templates := registry.All()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tTIER\tLANGUAGES\tSNAPSHOT")

	for _, tmpl := range templates {
		exists, err := store.Exists(ctx, tmpl.ID)
		snapStatus := "missing"
		if err != nil {
			snapStatus = "error: " + err.Error()
		} else if exists {
			snapStatus = "ok"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			tmpl.ID, tmpl.Tier, strings.Join(tmpl.Languages, ","), snapStatus)
	}

	return w.Flush()
}

func initDeps(cfg app.Config) (storage.SnapshotStore, *template.Registry, *logging.JSONLogger, error) {
	logger := logging.NewJSONLogger("snapshot-builder", os.Stdout)

	bucketOpener, err := newSnapshotBucketOpener(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	if bucketOpener == nil {
		return nil, nil, nil, fmt.Errorf("storage backend not configured")
	}
	if cfg.TemplatesPath == "" {
		return nil, nil, nil, fmt.Errorf("templates path not configured (set ARKLOOP_SANDBOX_TEMPLATES_PATH)")
	}

	cacheDir := cfg.SocketBaseDir + "/_snapshots"
	store, err := storage.NewSnapshotStore(context.Background(), bucketOpener, cacheDir)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("init snapshot store: %w", err)
	}

	registry, err := template.LoadFromFile(cfg.TemplatesPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load templates: %w", err)
	}

	return store, registry, logger, nil
}

func newSnapshotBucketOpener(cfg app.Config) (objectstore.BucketOpener, error) {
	runtimeConfig, err := objectstore.NormalizeRuntimeConfig(objectstore.RuntimeConfig{
		Backend: cfg.StorageBackend,
		RootDir: cfg.StorageRoot,
		S3Config: objectstore.S3Config{
			Endpoint:  cfg.S3Endpoint,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	if !runtimeConfig.Enabled() {
		return nil, nil
	}
	return runtimeConfig.BucketOpener()
}

func initNetworkManager(cfg app.Config) (*firecracker.NetworkManager, error) {
	manager, err := firecracker.NewNetworkManager(firecracker.NetworkConfig{
		AllowEgress:     cfg.AllowEgress,
		EgressInterface: cfg.FirecrackerEgressInterface,
		TapPrefix:       cfg.FirecrackerTapPrefix,
		AddressPoolCIDR: cfg.FirecrackerTapCIDR,
		Nameservers:     cfg.FirecrackerDNS,
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := manager.ValidateHost(ctx); err != nil {
		return nil, err
	}
	version, err := firecracker.DetectVersion(ctx, cfg.FirecrackerBin)
	if err != nil {
		return nil, err
	}
	if version.Less(firecracker.MinSnapshotTapPatchVersion) {
		return nil, fmt.Errorf("firecracker version must be >= %d.%d.%d for snapshot network restore", firecracker.MinSnapshotTapPatchVersion.Major, firecracker.MinSnapshotTapPatchVersion.Minor, firecracker.MinSnapshotTapPatchVersion.Patch)
	}
	return manager, nil
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  snapshot-builder create --template <id>")
	fmt.Fprintln(os.Stderr, "  snapshot-builder create --all")
	fmt.Fprintln(os.Stderr, "  snapshot-builder list")
}
