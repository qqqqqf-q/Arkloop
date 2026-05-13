package manifest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"

	"arkloop/services/shared/pluginmanifest"
)

const (
	SchemaVersion        = pluginmanifest.SchemaVersion
	MaxBundleBytes int64 = 64 << 20
)

var versionSegmentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:+-]{0,127}$`)

type Manifest = pluginmanifest.Manifest

type Parsed struct {
	Manifest Manifest
	JSON     json.RawMessage
	YAML     []byte
}

func Parse(data []byte) (Parsed, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return Parsed{}, fmt.Errorf("manifest is empty")
	}

	manifest, err := pluginmanifest.Parse(data)
	if err != nil {
		return Parsed{}, err
	}
	if err := Validate(manifest); err != nil {
		return Parsed{}, err
	}
	encoded, err := manifest.ToManifestJSON()
	if err != nil {
		return Parsed{}, err
	}

	return Parsed{
		Manifest: manifest,
		JSON:     json.RawMessage(encoded),
		YAML:     append([]byte(nil), data...),
	}, nil
}

func Validate(m Manifest) error {
	if err := m.Validate(); err != nil {
		return err
	}
	if !versionSegmentPattern.MatchString(strings.TrimSpace(m.Version)) {
		return fmt.Errorf("version is invalid")
	}
	for _, platform := range m.Platforms {
		if strings.ContainsAny(platform, `/\`) {
			return fmt.Errorf("platform is invalid")
		}
	}
	return nil
}

func ExtractManifestFromBundle(bundle []byte) ([]byte, error) {
	var manifest []byte
	err := walkBundle(bundle, func(tr *tar.Reader, hdr *tar.Header, name string) error {
		if !isBundleManifestName(name) {
			return nil
		}
		if hdr.Typeflag == tar.TypeDir {
			return fmt.Errorf("%s must be a file", name)
		}
		if manifest != nil {
			return fmt.Errorf("plugin manifest is duplicated")
		}
		if hdr.Size > 1<<20 {
			return fmt.Errorf("plugin manifest is too large")
		}
		data, err := io.ReadAll(io.LimitReader(tr, hdr.Size+1))
		if err != nil {
			return fmt.Errorf("read plugin manifest: %w", err)
		}
		if int64(len(data)) != hdr.Size {
			return fmt.Errorf("plugin manifest size mismatch")
		}
		manifest = data
		return nil
	})
	if err != nil {
		return nil, err
	}
	if manifest == nil {
		return nil, fmt.Errorf("plugin manifest not found")
	}
	return manifest, nil
}

func isBundleManifestName(name string) bool {
	switch strings.Trim(strings.ReplaceAll(name, "\\", "/"), "/") {
	case "manifest.yaml", "manifest.yml", "manifest.json", "plugin.json", ".codex-plugin/plugin.json":
		return true
	default:
		return false
	}
}

func ValidateBundle(bundle []byte) error {
	return walkBundle(bundle, func(_ *tar.Reader, _ *tar.Header, _ string) error {
		return nil
	})
}

func walkBundle(bundle []byte, visit func(*tar.Reader, *tar.Header, string) error) error {
	gz, err := gzip.NewReader(bytes.NewReader(bundle))
	if err != nil {
		return fmt.Errorf("open gzip bundle: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar bundle: %w", err)
		}
		name, err := cleanTarPath(hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeReg, tar.TypeRegA, tar.TypeDir:
		default:
			return fmt.Errorf("unsupported tar entry type for %s", name)
		}
		if err := visit(tr, hdr, name); err != nil {
			return err
		}
	}
	return nil
}

func cleanTarPath(raw string) (string, error) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" {
		return "", fmt.Errorf("tar entry path is empty")
	}
	if strings.HasPrefix(raw, "/") {
		return "", fmt.Errorf("tar entry path is absolute")
	}
	cleaned := path.Clean(raw)
	if cleaned == "." || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", fmt.Errorf("tar entry path escapes bundle")
	}
	return cleaned, nil
}

func validateBundlePath(raw string) error {
	_, err := cleanTarPath(raw)
	return err
}
