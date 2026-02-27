package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"arkloop/services/sandbox/internal/app"
	"arkloop/services/sandbox/internal/logging"
	"arkloop/services/sandbox/internal/snapshot"
	"arkloop/services/sandbox/internal/storage"
	"arkloop/services/sandbox/internal/template"
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
	templateID := fs.String("template", "", "template ID to build (e.g. python3.12-lite)")
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

	builder := snapshot.NewBuilder(
		cfg.FirecrackerBin,
		cfg.SocketBaseDir,
		cfg.BootTimeoutSeconds,
		cfg.GuestAgentPort,
		store,
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

	if cfg.S3Endpoint == "" {
		return nil, nil, nil, fmt.Errorf("S3 endpoint not configured (set ARKLOOP_S3_ENDPOINT)")
	}
	if cfg.TemplatesPath == "" {
		return nil, nil, nil, fmt.Errorf("templates path not configured (set ARKLOOP_SANDBOX_TEMPLATES_PATH)")
	}

	cacheDir := cfg.SocketBaseDir + "/_snapshots"
	store, err := storage.NewMinIOStore(context.Background(), cfg.S3Endpoint, cfg.S3AccessKey, cfg.S3SecretKey, cacheDir)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("init minio store: %w", err)
	}

	registry, err := template.LoadFromFile(cfg.TemplatesPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load templates: %w", err)
	}

	return store, registry, logger, nil
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  snapshot-builder create --template <id>")
	fmt.Fprintln(os.Stderr, "  snapshot-builder create --all")
	fmt.Fprintln(os.Stderr, "  snapshot-builder list")
}
