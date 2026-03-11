package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"arkloop/services/api/internal/app"
	"arkloop/services/api/internal/data"
	"arkloop/services/api/internal/deorg"

	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	if err := run(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
}

func run() error {
	if _, err := app.LoadDotenvIfEnabled(false); err != nil {
		return err
	}
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: go run ./cmd/deorg <export|import> [flags]")
	}
	ctx := context.Background()
	switch strings.TrimSpace(os.Args[1]) {
	case "export":
		return runExport(ctx, os.Args[2:])
	case "import":
		return runImport(ctx, os.Args[2:])
	default:
		return fmt.Errorf("unknown command: %s", os.Args[1])
	}
}

func runExport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("out", "-", "output file, use - for stdout")
	dsn := fs.String("dsn", resolveDSN(), "postgres dsn")
	if err := fs.Parse(args); err != nil {
		return err
	}
	pool, err := openPool(ctx, *dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	manifest, err := deorg.Export(ctx, pool)
	if err != nil {
		return err
	}
	return writeManifest(*file, manifest)
}

func runImport(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	file := fs.String("in", "-", "input file, use - for stdin")
	dsn := fs.String("dsn", resolveDSN(), "postgres dsn")
	if err := fs.Parse(args); err != nil {
		return err
	}
	manifest, err := readManifest(*file)
	if err != nil {
		return err
	}
	pool, err := openPool(ctx, *dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	return deorg.Import(ctx, pool, manifest)
}

func openPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, fmt.Errorf("ARKLOOP_DATABASE_URL (or DATABASE_URL) is required")
	}
	normalized := data.NormalizePostgresDSN(dsn)
	pool, err := pgxpool.New(ctx, normalized)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	return pool, nil
}

func writeManifest(path string, manifest deorg.Manifest) error {
	writer := io.Writer(os.Stdout)
	if path != "-" {
		file, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("create output: %w", err)
		}
		defer file.Close()
		writer = file
	}
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(manifest); err != nil {
		return fmt.Errorf("encode manifest: %w", err)
	}
	return nil
}

func readManifest(path string) (deorg.Manifest, error) {
	reader := io.Reader(os.Stdin)
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return deorg.Manifest{}, fmt.Errorf("open input: %w", err)
		}
		defer file.Close()
		reader = file
	}
	var manifest deorg.Manifest
	if err := json.NewDecoder(reader).Decode(&manifest); err != nil {
		return deorg.Manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	return manifest, nil
}

func resolveDSN() string {
	for _, key := range []string{"ARKLOOP_DATABASE_URL", "DATABASE_URL"} {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
