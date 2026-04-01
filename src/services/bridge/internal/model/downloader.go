package model

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"arkloop/services/bridge/internal/docker"
)

const installedVariantFilename = ".installed_variant"

// Variant describes a downloadable model variant.
type Variant struct {
	ID             string // "22m" or "86m"
	Name           string
	Image          string      // OCI image containing model files
	ModelScopeRepo string      // ModelScope repo for fallback download
	HFRepo         string      // HuggingFace repo for wget-based download
	HFFiles        [][2]string // [remote_path, local_name] pairs
	Size           string      // human-readable size hint
}

// Variants is the registry of known prompt-guard model variants.
var Variants = map[string]Variant{
	"22m": {
		ID:             "22m",
		Name:           "Prompt Guard 2 (22M)",
		Image:          "ghcr.io/arkloop/prompt-guard-22m-onnx:latest",
		ModelScopeRepo: "LLM-Research/Llama-Prompt-Guard-2-22M",
		HFRepo:         "protectai/deberta-v3-base-prompt-injection-v2",
		HFFiles: [][2]string{
			{"onnx/model.onnx", "model.onnx"},
			{"onnx/tokenizer.json", "tokenizer.json"},
		},
		Size: "~270 MB",
	},
	"86m": {
		ID:             "86m",
		Name:           "Prompt Guard (86M)",
		Image:          "ghcr.io/arkloop/prompt-guard-86m-onnx:latest",
		ModelScopeRepo: "LLM-Research/Llama-Prompt-Guard-2-86M",
		Size:           "~690 MB",
	},
}

// Logger is the interface for structured logging.
type Logger interface {
	Info(msg string, extra map[string]any)
	Error(msg string, extra map[string]any)
}

// Downloader handles model file distribution via OCI images.
type Downloader struct {
	modelDir string
	logger   Logger
	mu       sync.Mutex
	busy     bool
}

// NewDownloader creates a model downloader targeting the given directory.
// If modelDir is empty it defaults to /var/lib/arkloop/models/prompt-guard.
func NewDownloader(modelDir string, logger Logger) *Downloader {
	if modelDir == "" {
		dataDir := os.Getenv("ARKLOOP_DATA_DIR")
		if dataDir == "" {
			dataDir = "/var/lib/arkloop"
		}
		modelDir = dataDir + "/models/prompt-guard"
	}
	return &Downloader{modelDir: modelDir, logger: logger}
}

// Install pulls the model variant image, extracts model files, and writes
// them to the configured model directory. Returns an Operation that can be
// tracked via the operation stream endpoint.
func (d *Downloader) Install(ctx context.Context, variantID string) (*docker.Operation, error) {
	v, ok := Variants[variantID]
	if !ok {
		return nil, fmt.Errorf("unknown model variant %q (valid: 22m, 86m)", variantID)
	}

	// Allow overriding the image via env var for dev/testing.
	if override := os.Getenv("ARKLOOP_PROMPT_GUARD_IMAGE_" + strings.ToUpper(variantID)); override != "" {
		v.Image = override
	}

	d.mu.Lock()
	if d.busy {
		d.mu.Unlock()
		return nil, fmt.Errorf("prompt-guard model install already in progress")
	}
	d.busy = true
	d.mu.Unlock()

	op := docker.NewOperation("prompt-guard", "install")
	op.Status = docker.OperationRunning

	cancelCtx, cancel := context.WithCancel(ctx)
	op.SetCancelFunc(cancel)

	go func() {
		defer func() {
			d.mu.Lock()
			d.busy = false
			d.mu.Unlock()
		}()
		installedVariant := d.InstalledVariant()
		// 进入 goroutine 后再次检查，避免并发 Install 之间的竞态
		if d.ModelFilesExist() && installedVariant == v.ID {
			op.AppendLog(fmt.Sprintf("model variant %s already installed, skipping download", v.ID))
			op.Complete(nil)
			return
		}
		err := d.download(cancelCtx, op, v, installedVariant)
		op.Complete(err)
	}()

	return op, nil
}

func (d *Downloader) download(ctx context.Context, op *docker.Operation, v Variant, installedVariant string) error {
	op.AppendLog(fmt.Sprintf("variant: %s (%s)", v.Name, v.Size))

	if err := os.MkdirAll(d.modelDir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", d.modelDir, err)
	}
	op.AppendLog("model directory: " + d.modelDir)

	if d.ModelFilesExist() {
		switch {
		case installedVariant == "":
			op.AppendLog("existing model files found without variant metadata; reinstalling requested variant")
		case installedVariant != v.ID:
			op.AppendLog(fmt.Sprintf("replacing installed variant %s with requested variant %s", installedVariant, v.ID))
		default:
			op.AppendLog(fmt.Sprintf("model files for variant %s already present, skipping download", v.ID))
			d.logger.Info("prompt-guard model already installed", map[string]any{
				"variant": v.ID, "model_dir": d.modelDir,
			})
			return nil
		}
	}

	// Strategy 1: Docker image pull + extract.
	op.AppendLog(fmt.Sprintf("trying docker pull %s ...", v.Image))
	if err := d.tryDockerInstall(ctx, op, v); err == nil {
		return d.verifyFiles(op, v)
	} else {
		op.AppendLog(fmt.Sprintf("docker pull failed: %s", err))
	}

	// Strategy 2: wget from HuggingFace (requires no special tools).
	if v.HFRepo != "" && len(v.HFFiles) > 0 {
		hfRepo := v.HFRepo
		if override := os.Getenv("ARKLOOP_PROMPT_GUARD_HF_REPO_" + strings.ToUpper(v.ID)); override != "" {
			hfRepo = override
		}
		op.AppendLog(fmt.Sprintf("trying HuggingFace download: %s", hfRepo))
		if err := d.tryHFInstall(ctx, op, hfRepo, v.HFFiles); err == nil {
			return d.verifyFiles(op, v)
		} else {
			op.AppendLog(fmt.Sprintf("HuggingFace download failed: %s", err))
		}
	}

	// Strategy 3: ModelScope download + ONNX export (requires modelscope + optimum).
	if v.ModelScopeRepo != "" {
		op.AppendLog(fmt.Sprintf("falling back to modelscope: %s", v.ModelScopeRepo))
		if err := d.tryModelScopeInstall(ctx, op, v); err == nil {
			return d.verifyFiles(op, v)
		} else {
			op.AppendLog(fmt.Sprintf("modelscope failed: %s", err))
			return fmt.Errorf("all download methods failed for %s", v.ID)
		}
	}

	return fmt.Errorf("no download method succeeded for %s", v.ID)
}

// ModelFilesExist reports whether model.onnx and tokenizer.json exist.
func (d *Downloader) ModelFilesExist() bool {
	for _, f := range []string{"model.onnx", "tokenizer.json"} {
		if _, err := os.Stat(filepath.Join(d.modelDir, f)); err != nil {
			return false
		}
	}
	return true
}

// InstalledVariant reports the persisted Prompt Guard variant ID, or "" when
// the current files predate variant metadata or the metadata is unreadable.
func (d *Downloader) InstalledVariant() string {
	raw, err := os.ReadFile(filepath.Join(d.modelDir, installedVariantFilename))
	if err != nil {
		return ""
	}
	variantID := strings.TrimSpace(string(raw))
	if _, ok := Variants[variantID]; !ok {
		return ""
	}
	return variantID
}

func (d *Downloader) verifyFiles(op *docker.Operation, v Variant) error {
	for _, f := range []string{"model.onnx", "tokenizer.json"} {
		p := filepath.Join(d.modelDir, f)
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("expected file missing: %s", p)
		}
	}
	if err := os.WriteFile(filepath.Join(d.modelDir, installedVariantFilename), []byte(v.ID+"\n"), 0644); err != nil {
		return fmt.Errorf("write installed variant: %w", err)
	}
	op.AppendLog("model installed to " + d.modelDir)
	d.logger.Info("prompt-guard model installed", map[string]any{
		"variant": v.ID, "model_dir": d.modelDir,
	})
	return nil
}

func (d *Downloader) tryDockerInstall(ctx context.Context, op *docker.Operation, v Variant) error {
	if err := d.run(ctx, op, "docker", "pull", v.Image); err != nil {
		return err
	}
	containerName := "arkloop-model-extract-" + v.ID
	_ = d.run(ctx, op, "docker", "rm", "-f", containerName)

	op.AppendLog("extracting model files...")
	if err := d.run(ctx, op, "docker", "create", "--name", containerName, v.Image); err != nil {
		return err
	}
	src := containerName + ":/models/."
	err := d.run(ctx, op, "docker", "cp", src, d.modelDir)
	_ = d.run(ctx, op, "docker", "rm", "-f", containerName)
	return err
}

func (d *Downloader) tryHFInstall(ctx context.Context, op *docker.Operation, repo string, files [][2]string) error {
	for _, pair := range files {
		remotePath, localName := pair[0], pair[1]
		url := fmt.Sprintf("https://huggingface.co/%s/resolve/main/%s", repo, remotePath)
		dest := filepath.Join(d.modelDir, localName)
		op.AppendLog(fmt.Sprintf("downloading %s ...", localName))
		if err := d.run(ctx, op, "wget", "-O", dest, url); err != nil {
			return fmt.Errorf("download %s: %w", remotePath, err)
		}
		info, err := os.Stat(dest)
		if err != nil || info.Size() == 0 {
			return fmt.Errorf("downloaded file empty or missing: %s", dest)
		}
		op.AppendLog(fmt.Sprintf("%s: %d bytes", localName, info.Size()))
	}
	return nil
}

func (d *Downloader) tryModelScopeInstall(ctx context.Context, op *docker.Operation, v Variant) error {
	tmpDir := filepath.Join(os.TempDir(), "arkloop-prompt-guard-"+v.ID)
	onnxDir := tmpDir + "-onnx"

	// Download from ModelScope.
	op.AppendLog("downloading model weights...")
	if err := d.run(ctx, op, "modelscope", "download",
		"--model", v.ModelScopeRepo,
		"--local_dir", tmpDir,
	); err != nil {
		return fmt.Errorf("modelscope download: %w", err)
	}

	// Export to ONNX via optimum.
	op.AppendLog("exporting to ONNX format...")
	script := fmt.Sprintf(
		"from optimum.onnxruntime import ORTModelForSequenceClassification; "+
			"m = ORTModelForSequenceClassification.from_pretrained(%q, export=True); "+
			"m.save_pretrained(%q); print('onnx_export_ok')",
		tmpDir, onnxDir,
	)
	if err := d.run(ctx, op, "python3", "-c", script); err != nil {
		return fmt.Errorf("ONNX export: %w", err)
	}

	// Copy files to target.
	onnxModel := filepath.Join(onnxDir, "model.onnx")
	tokenizer := filepath.Join(tmpDir, "tokenizer.json")
	for _, pair := range [][2]string{
		{onnxModel, filepath.Join(d.modelDir, "model.onnx")},
		{tokenizer, filepath.Join(d.modelDir, "tokenizer.json")},
	} {
		data, err := os.ReadFile(pair[0])
		if err != nil {
			return fmt.Errorf("read %s: %w", pair[0], err)
		}
		if err := os.WriteFile(pair[1], data, 0644); err != nil {
			return fmt.Errorf("write %s: %w", pair[1], err)
		}
	}

	// 清理临时目录
	if err := os.RemoveAll(tmpDir); err != nil {
		d.logger.Error("cleanup temp dir failed", map[string]any{
			"path": tmpDir, "error": err.Error(),
		})
	}
	if err := os.RemoveAll(onnxDir); err != nil {
		d.logger.Error("cleanup onnx dir failed", map[string]any{
			"path": onnxDir, "error": err.Error(),
		})
	}

	op.AppendLog("ONNX export and copy complete")
	return nil
}

// run executes a command, streams stdout/stderr to the operation log, and
// returns the exit error (if any).
func (d *Downloader) run(ctx context.Context, op *docker.Operation, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	configureCommand(cmd)

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		return err
	}
	if cmd.Process != nil {
		op.SetPID(cmd.Process.Pid)
	}

	scanner := bufio.NewScanner(pipe)
	for scanner.Scan() {
		op.AppendLog(scanner.Text())
	}
	return cmd.Wait()
}
