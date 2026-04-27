package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"localsync/shared/update"
)

var rootCmd = &cobra.Command{
	Use:   "batchcompress",
	Short: "Batch-compress videos into .localsync/ variant folders",
	RunE:  run,
}

func init() {
	rootCmd.Flags().StringP("config", "c", defaultConfigPath(), "path to config.toml")
	rootCmd.Flags().StringP("input", "i", "", "input folder or file (required)")
	rootCmd.Flags().StringP("preset", "p", "720p", "preset name from [batchcompress.preset]")
	rootCmd.Flags().Bool("all", false, "select all discovered files, skip the picker")
	rootCmd.Flags().BoolP("recursive", "r", false, "scan subdirectories")
	rootCmd.Flags().BoolP("version", "v", false, "print version and exit")
	rootCmd.Flags().BoolP("update", "u", false, "update to the latest release")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, _ []string) error {
	if showVer, _ := cmd.Flags().GetBool("version"); showVer {
		fmt.Println("batchcompress", update.Version)
		return nil
	}

	if doUpdate, _ := cmd.Flags().GetBool("update"); doUpdate {
		return update.SelfUpdate("batchcompress")
	}

	configPath, _ := cmd.Flags().GetString("config")
	inputPath, _ := cmd.Flags().GetString("input")
	presetName, _ := cmd.Flags().GetString("preset")
	all, _ := cmd.Flags().GetBool("all")
	recursive, _ := cmd.Flags().GetBool("recursive")

	if inputPath == "" {
		return fmt.Errorf("--input is required")
	}

	tc, err := LoadBatchConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	preset := tc.FindPreset(presetName)
	if preset == nil && len(tc.Preset) == 1 {
		// Only one preset defined — use it regardless of the flag value.
		preset = &tc.Preset[0]
	}
	if preset == nil {
		names := make([]string, 0, len(tc.Preset))
		for _, p := range tc.Preset {
			names = append(names, p.Name)
		}
		return fmt.Errorf("preset %q not found in [batchcompress.preset]; available: %s",
			presetName, strings.Join(names, ", "))
	}

	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return fmt.Errorf("ffmpeg not found on PATH: %w", err)
	}
	hasFfprobe := false
	if _, err := exec.LookPath("ffprobe"); err == nil {
		hasFfprobe = true
	}

	absInput, err := filepath.Abs(inputPath)
	if err != nil {
		return fmt.Errorf("resolve input path: %w", err)
	}
	fi, err := os.Stat(absInput)
	if err != nil {
		return fmt.Errorf("stat input: %w", err)
	}

	var paths []string
	switch {
	case !fi.IsDir():
		paths = []string{absInput}
	case all:
		files, err := Scan(absInput, recursive)
		if err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		for _, f := range files {
			paths = append(paths, f.Path)
		}
		if len(paths) == 0 {
			return fmt.Errorf("no video files found in %s", absInput)
		}
	default:
		var ok bool
		paths, ok = RunPicker(absInput, recursive)
		if !ok {
			fmt.Println("cancelled")
			return nil
		}
		if len(paths) == 0 {
			fmt.Println("no files selected")
			return nil
		}
	}

	fmtName := tc.Format
	if len(tc.Command) > 0 {
		for i := 0; i+1 < len(tc.Command); i++ {
			if tc.Command[i] == "-f" {
				fmtName = tc.Command[i+1]
				break
			}
		}
	}
	ext := formatExtensions[fmtName]
	if ext == "" {
		ext = "mkv"
	}

	queue := make([]*QueueItem, 0, len(paths))
	for _, src := range paths {
		stem := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
		outDir := filepath.Join(filepath.Dir(src), ".localsync")
		outName := fmt.Sprintf("%s_%s.%s", stem, preset.Name, ext)
		item := &QueueItem{
			Job: Job{
				Source:     src,
				OutputDir:  outDir,
				OutputName: outName,
				Preset:     *preset,
				Tc:         tc,
			},
			Status: StatusPending,
		}
		if _, err := os.Stat(item.Job.OutputPath()); err == nil {
			item.Status = StatusPreExisting
		}
		queue = append(queue, item)
	}

	ctrl := &Controller{}
	dash := NewDashboard(preset.Name, queue, ctrl)

	logPath := filepath.Join(filepath.Dir(configPath), "batchcompress.log")
	logFile, logErr := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if logErr != nil {
		// Fall back silently — log goes to TUI only.
		logPath = ""
	} else {
		defer logFile.Close()
		fmt.Fprintf(logFile, "\n=== %s  input=%s  preset=%s ===\n",
			time.Now().Format(time.RFC3339), absInput, preset.Name)
	}

	var logWriter io.Writer = dash.LogWriter()
	if logFile != nil {
		logWriter = io.MultiWriter(logFile, dash.LogWriter())
	}
	// Route log.Printf into the same sink so messages don't garble the TUI.
	prevLogOut := log.Writer()
	prevLogFlags := log.Flags()
	log.SetOutput(logWriter)
	log.SetFlags(log.LstdFlags)
	defer func() {
		log.SetOutput(prevLogOut)
		log.SetFlags(prevLogFlags)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-sigs:
			ctrl.Kill()
			cancel()
			dash.Stop()
		case <-ctx.Done():
		}
	}()
	go func() {
		select {
		case <-dash.QuitChan():
			cancel()
		case <-ctx.Done():
		}
	}()

	total := len(queue)
	log.Printf("queue: %d files; preset=%s; format=%s", total, preset.Name, tc.Format)

	go func() {
		defer dash.Stop()
		for i, item := range queue {
			if ctx.Err() != nil {
				return
			}
			tag := fmt.Sprintf("[%d/%d]", i+1, total)
			if item.Status == StatusPreExisting {
				log.Printf("%s skip (output exists): %s", tag, item.Job.OutputPath())
				continue
			}
			// Drain any stale skip request from before this job started.
			select {
			case <-dash.SkipChan():
			default:
			}
			log.Printf("%s starting: %s", tag, item.Job.Source)
			dash.SetStatus(i, StatusActive, "")
			if hasFfprobe {
				if dur, err := probeDuration(ctx, item.Job.Source); err == nil {
					dash.SetDuration(i, dur)
					log.Printf("%s duration: %s", tag, dur.Round(time.Second))
				}
			}
			progressCh := make(chan Progress, 16)
			progDone := make(chan struct{})
			go func(idx int) {
				for p := range progressCh {
					dash.UpdateProgress(idx, p)
				}
				close(progDone)
			}(i)

			start := time.Now()
			err := item.Job.Run(ctx, ctrl, progressCh, logWriter)
			close(progressCh)
			<-progDone
			elapsed := time.Since(start).Round(time.Second)

			skipped := false
			select {
			case <-dash.SkipChan():
				skipped = true
			default:
			}

			switch {
			case skipped:
				log.Printf("%s skipped after %s: %s", tag, elapsed, item.Job.Source)
				dash.SetStatus(i, StatusSkipped, "")
			case err != nil && ctx.Err() != nil:
				log.Printf("%s cancelled after %s: %s", tag, elapsed, item.Job.Source)
				dash.SetStatus(i, StatusSkipped, "cancelled")
				return
			case err != nil:
				log.Printf("%s ERROR after %s: %s — %v", tag, elapsed, item.Job.Source, err)
				dash.SetStatus(i, StatusError, err.Error())
			default:
				log.Printf("%s done in %s: %s", tag, elapsed, item.Job.OutputPath())
				dash.SetStatus(i, StatusDone, "")
			}
		}
		log.Printf("queue complete")
	}()

	dashErr := dash.Run(ctx)
	cancel()
	time.Sleep(200 * time.Millisecond)

	printSummary(os.Stdout, queue, logPath, dashErr)
	return dashErr
}

func printSummary(w io.Writer, queue []*QueueItem, logPath string, dashErr error) {
	var done, skipped, errored, existed, pending int
	for _, q := range queue {
		switch q.Status {
		case StatusDone:
			done++
		case StatusSkipped:
			skipped++
		case StatusError:
			errored++
		case StatusPreExisting:
			existed++
		case StatusPending:
			pending++
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "=== batchcompress summary ===")
	fmt.Fprintf(w, "  done: %d   skipped: %d   errored: %d   already-existed: %d   pending: %d\n",
		done, skipped, errored, existed, pending)

	if errored > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "errors:")
		for i, q := range queue {
			if q.Status == StatusError {
				fmt.Fprintf(w, "  [%d] %s\n      %s\n", i+1, q.Job.Source, q.Err)
			}
		}
	}
	if dashErr != nil {
		fmt.Fprintf(w, "\ndashboard error: %v\n", dashErr)
	}
	if logPath != "" {
		fmt.Fprintf(w, "\nfull ffmpeg log: %s\n", logPath)
	}
}

func probeDuration(ctx context.Context, path string) (time.Duration, error) {
	out, err := exec.CommandContext(ctx, "ffprobe",
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=nw=1:nk=1",
		path).Output()
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(out))
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return time.Duration(f * float64(time.Second)), nil
}
