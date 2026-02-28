package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/lazypower/spark-tools/pkg/hfetch/api"
	"github.com/lazypower/spark-tools/pkg/hfetch/config"
	"github.com/lazypower/spark-tools/pkg/hfetch/download"
	"github.com/lazypower/spark-tools/pkg/hfetch/gguf"
	"github.com/lazypower/spark-tools/pkg/hfetch/registry"
)

type pullFlags struct {
	quant        string
	output       string
	streams      int
	maxBandwidth string
	allFiles     bool
	jsonOutput   bool
	filename     string
}

func pullCmd() *cobra.Command {
	var flags pullFlags

	cmd := &cobra.Command{
		Use:   "pull <model_id> [filename]",
		Short: "Download a model",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				flags.filename = args[1]
			}
			return runPull(cmd, args[0], flags)
		},
	}

	cmd.Flags().StringVar(&flags.quant, "quant", "", "Auto-select file by quantization type")
	cmd.Flags().StringVar(&flags.output, "output", "", "Override download directory")
	cmd.Flags().IntVar(&flags.streams, "streams", 0, "Parallel download streams (default: 4, or HFETCH_STREAMS)")
	cmd.Flags().StringVar(&flags.maxBandwidth, "max-bandwidth", "", "Bandwidth limit (e.g. \"100MB/s\")")
	cmd.Flags().BoolVar(&flags.allFiles, "all-files", false, "Show all files in picker, not just GGUF")
	cmd.Flags().BoolVar(&flags.jsonOutput, "json", false, "Machine-readable JSON progress output")
	cmd.Flags().Bool("verify", true, "Re-verify SHA256 after download")
	tokenFlag(cmd)
	return cmd
}

func resolveStreams(flagValue int) int {
	if flagValue > 0 {
		return flagValue
	}
	if v := os.Getenv("HFETCH_STREAMS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 4
}

func runPull(cmd *cobra.Command, modelID string, flags pullFlags) error {
	client := newAPIClient(cmd)
	dirs := config.Dirs()

	// Get file list.
	files, err := client.ListFiles(context.Background(), modelID)
	if err != nil {
		return err
	}

	// Build FileInfo for GGUF filtering.
	var infos []gguf.FileInfo
	fileSizeMap := make(map[string]int64)
	for _, f := range files {
		size := f.Size
		if f.LFS != nil {
			size = f.LFS.Size
		}
		quant := gguf.ParseQuantFromFilename(f.Filename)
		infos = append(infos, gguf.FileInfo{
			Filename:      f.Filename,
			Size:          size,
			Quantization:  quant,
			BitsPerWeight: gguf.QuantBitsPerWeight[quant],
		})
		fileSizeMap[f.Filename] = size
	}

	// Determine which file to download.
	var selectedFile string

	switch {
	case flags.filename != "":
		selectedFile = flags.filename
	case flags.quant != "":
		ggufFiles := gguf.FilterGGUF(infos)
		matched := gguf.FilterByQuant(ggufFiles, flags.quant)
		if len(matched) == 0 {
			return fmt.Errorf("no file found for quantization %q", flags.quant)
		}
		selectedFile = matched[0].Filename
	default:
		// Interactive picker.
		candidates := infos
		if !flags.allFiles {
			ggufFiles := gguf.FilterGGUF(infos)
			if len(ggufFiles) > 0 {
				candidates = ggufFiles
			}
		}

		if len(candidates) == 0 {
			return fmt.Errorf("no GGUF files found in %s (use --all-files to show all)", modelID)
		}
		gguf.SortByQuality(candidates)

		var options []huh.Option[string]
		for _, f := range candidates {
			fit := gguf.EstimateFit(f.Size, nil, 0)
			fitLabel := ""
			if fit.Status != gguf.FitUnknown {
				fitLabel = "  " + fit.Label
			}
			qualLabel := ""
			if ql := gguf.QuantQualityLabel(f.Quantization); ql != "" {
				qualLabel = "  " + ql
			}
			label := fmt.Sprintf("%-10s %s%s%s", f.Quantization, formatSize(f.Size), fitLabel, qualLabel)
			if f.Quantization == "" {
				label = fmt.Sprintf("%-10s %s", f.Filename, formatSize(f.Size))
			}
			options = append(options, huh.NewOption(label, f.Filename))
		}

		var selected string
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(modelID).
					Description("Select a file to download").
					Options(options...).
					Value(&selected),
			),
		)
		if err := form.Run(); err != nil {
			return err
		}
		selectedFile = selected
	}

	if selectedFile == "" {
		return fmt.Errorf("no file selected")
	}

	// Set up output directory.
	reg := registry.New(dirs.Data)
	if err := reg.Load(); err != nil {
		return err
	}

	outputDir := flags.output
	if outputDir == "" {
		outputDir = reg.ModelDir(modelID)
	}

	streams := resolveStreams(flags.streams)

	if !flags.jsonOutput {
		headerStyle := lipgloss.NewStyle().Bold(true)
		fmt.Printf("\n  %s\n", headerStyle.Render("Downloading"))
		fmt.Printf("  Model:   %s\n", modelID)
		fmt.Printf("  File:    %s\n", selectedFile)
		if size, ok := fileSizeMap[selectedFile]; ok {
			fmt.Printf("  Size:    %s\n", formatSize(size))
		}
		fmt.Printf("  Streams: %d\n", streams)
		fmt.Println()
	}

	// Create an API-backed file source.
	src := &apiFileSource{
		client:  client,
		modelID: modelID,
		file:    selectedFile,
	}

	startTime := time.Now()

	var progressFn download.ProgressFunc
	if flags.jsonOutput {
		progressFn = func(e download.ProgressEvent) {
			evt := map[string]any{
				"file":            e.File,
				"bytes_completed": e.BytesCompleted,
				"bytes_total":     e.BytesTotal,
				"speed_bps":       e.Speed,
				"phase":           e.Phase,
			}
			data, _ := json.Marshal(evt)
			fmt.Println(string(data))
		}
	} else {
		progressFn = func(e download.ProgressEvent) {
			switch e.Phase {
			case "downloading":
				pct := float64(e.BytesCompleted) / float64(e.BytesTotal) * 100
				speed := ""
				if e.Speed > 0 {
					speed = fmt.Sprintf(" %s/s", formatSize(int64(e.Speed)))
				}
				elapsed := time.Since(startTime)
				eta := ""
				if e.Speed > 0 && e.BytesCompleted < e.BytesTotal {
					remaining := float64(e.BytesTotal-e.BytesCompleted) / e.Speed
					eta = fmt.Sprintf(" ETA %s", time.Duration(remaining*float64(time.Second)).Truncate(time.Second))
				}
				_ = elapsed
				fmt.Printf("\r  Downloading... %.1f%% (%s / %s)%s%s    ",
					pct, formatSize(e.BytesCompleted), formatSize(e.BytesTotal), speed, eta)
			case "verifying":
				fmt.Printf("\r  Verifying SHA256...                                              ")
			case "complete":
				fmt.Printf("\r  Complete! %s                                                     \n", formatSize(e.BytesTotal))
			}
		}
	}

	var maxBW int64
	if flags.maxBandwidth != "" {
		maxBW, err = parseBandwidth(flags.maxBandwidth)
		if err != nil {
			return fmt.Errorf("invalid --max-bandwidth %q: %w", flags.maxBandwidth, err)
		}
	}

	finalPath, err := download.Download(context.Background(), src, selectedFile, download.Options{
		OutputDir:    outputDir,
		Streams:      streams,
		MaxBandwidth: maxBW,
		OnProgress:   progressFn,
	})
	if err != nil {
		if !flags.jsonOutput {
			// Ensure error is visible after \r progress output.
			fmt.Println()
		}
		return err
	}

	// Register the downloaded file.
	quant := gguf.ParseQuantFromFilename(selectedFile)
	reg.AddFile(modelID, registry.LocalFile{
		Filename:     selectedFile,
		Size:         fileSizeMap[selectedFile],
		Quantization: quant,
		LocalPath:    finalPath,
		Complete:     true,
		DownloadedAt: time.Now(),
	})
	if err := reg.Save(); err != nil {
		return fmt.Errorf("saving manifest: %w", err)
	}

	if flags.jsonOutput {
		evt := map[string]any{
			"phase": "saved",
			"path":  finalPath,
			"model": modelID,
			"file":  selectedFile,
		}
		data, _ := json.Marshal(evt)
		fmt.Println(string(data))
	} else {
		fmt.Printf("  Saved to: %s\n\n", finalPath)
	}
	return nil
}

// apiFileSource adapts the HF API client to the download.FileSource interface.
type apiFileSource struct {
	client  *api.Client
	modelID string
	file    string
}

func (s *apiFileSource) Head(ctx context.Context) (int64, string, error) {
	return s.client.HeadFile(ctx, s.modelID, s.file)
}

func (s *apiFileSource) Download(ctx context.Context, offset int64) (io.ReadCloser, int64, error) {
	rc, size, err := s.client.DownloadFile(ctx, s.modelID, s.file, offset)
	if err != nil {
		if api.IsRangeNotSupported(err) {
			return nil, 0, download.ErrRangeNotSupported
		}
		return nil, 0, err
	}
	return rc, size, nil
}

// redactToken shows only the first 8 characters of a token.
func redactToken(token string) string {
	if len(token) <= 8 {
		return token + "..."
	}
	return token[:8] + "..."
}

// tokenSourceLabel returns a human-readable label for a token source.
func tokenSourceLabel(source string) string {
	switch source {
	case "flag":
		return "--token flag"
	case "env":
		return "HFETCH_TOKEN environment variable"
	case "config":
		dirs := config.Dirs()
		return dirs.Config + "/token.json (via hfetch login)"
	case "hf-compat":
		return "~/.cache/huggingface/token (HuggingFace CLI compat)"
	default:
		return "none"
	}
}

// parseBandwidth parses a human-readable bandwidth string like "100MB/s"
// or "50mb" into bytes per second. Supported suffixes: KB, MB, GB (case-insensitive).
func parseBandwidth(s string) (int64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "/s")
	s = strings.TrimSpace(s)

	upper := strings.ToUpper(s)
	multiplier := int64(1)
	numStr := s

	switch {
	case strings.HasSuffix(upper, "GB"):
		multiplier = 1024 * 1024 * 1024
		numStr = s[:len(s)-2]
	case strings.HasSuffix(upper, "MB"):
		multiplier = 1024 * 1024
		numStr = s[:len(s)-2]
	case strings.HasSuffix(upper, "KB"):
		multiplier = 1024
		numStr = s[:len(s)-2]
	case strings.HasSuffix(upper, "B"):
		numStr = s[:len(s)-1]
	}

	numStr = strings.TrimSpace(numStr)
	val, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse number: %w", err)
	}
	if val <= 0 {
		return 0, fmt.Errorf("bandwidth must be positive")
	}
	return int64(val * float64(multiplier)), nil
}
