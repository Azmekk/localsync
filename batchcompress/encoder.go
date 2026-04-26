package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

type Job struct {
	Source     string
	OutputDir  string
	OutputName string
	Preset     BatchPreset
	Tc         BatchTranscode
}

type Progress struct {
	OutTimeMs int64
	Speed     float64
	Bitrate   string
	FrameNum  int64
	Done      bool
}

var formatExtensions = map[string]string{
	"matroska": "mkv",
	"mp4":      "mp4",
	"webm":     "webm",
	"mpegts":   "ts",
	"mov":      "mov",
}

type Controller struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	pid    int
	paused bool
}

func (c *Controller) set(cmd *exec.Cmd) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cmd = cmd
	if cmd != nil && cmd.Process != nil {
		c.pid = cmd.Process.Pid
	} else {
		c.pid = 0
	}
	c.paused = false
}

func (c *Controller) clear() { c.set(nil) }

func (c *Controller) Pause() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pid == 0 || c.paused {
		return nil
	}
	if err := suspendPID(c.pid); err != nil {
		return err
	}
	c.paused = true
	return nil
}

func (c *Controller) Resume() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pid == 0 || !c.paused {
		return nil
	}
	if err := resumePID(c.pid); err != nil {
		return err
	}
	c.paused = false
	return nil
}

func (c *Controller) Toggle() (paused bool, err error) {
	c.mu.Lock()
	pid := c.pid
	wasPaused := c.paused
	c.mu.Unlock()
	if pid == 0 {
		return wasPaused, nil
	}
	if wasPaused {
		err = c.Resume()
		return false, err
	}
	err = c.Pause()
	return true, err
}

func (c *Controller) Paused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.paused
}

func (c *Controller) Kill() {
	c.mu.Lock()
	cmd := c.cmd
	pid := c.pid
	wasPaused := c.paused
	c.mu.Unlock()
	if cmd == nil || pid == 0 {
		return
	}
	// Resume first so suspended threads can process the kill signal.
	if wasPaused {
		_ = resumePID(pid)
		c.mu.Lock()
		c.paused = false
		c.mu.Unlock()
	}
	killTree(pid)
}

func (j *Job) OutputPath() string {
	return filepath.Join(j.OutputDir, j.OutputName)
}

func (j *Job) partialPath() string {
	return j.OutputPath() + ".partial"
}

func buildArgs(j *Job, outputPath string) []string {
	args := []string{"-y", "-i", j.Source, "-map", "0:v", "-map", "0:a"}
	if j.Tc.Subtitles {
		args = append(args, "-map", "0:s?")
	}

	if j.Preset.Resolution != "" {
		parts := strings.SplitN(j.Preset.Resolution, "x", 2)
		if len(parts) == 2 {
			args = append(args, "-vf", fmt.Sprintf("scale=-2:%s", parts[1]))
		}
	}

	if j.Preset.Bitrate != "" {
		args = append(args, "-b:v", j.Preset.Bitrate)
	}
	args = append(args, "-c:v", j.Tc.VideoCodec)

	extra := j.Preset.ExtraArgs
	if len(extra) == 0 {
		extra = j.Tc.ExtraArgs
	}
	args = append(args, extra...)

	hasACodec := containsFlag(extra, "-c:a")
	hasABitrate := containsFlag(extra, "-b:a")
	if !hasACodec {
		args = append(args, "-c:a", j.Tc.AudioCodec)
	}
	if !hasABitrate && !hasACodec && j.Tc.AudioCodec != "copy" {
		// If the user took control of audio codec, leave bitrate alone too.
		args = append(args, "-b:a", j.Tc.AudioBitrate)
	}

	if j.Tc.Subtitles {
		args = append(args, "-c:s", "copy")
	}

	args = append(args, "-progress", "pipe:1", "-nostats")
	args = append(args, "-f", j.Tc.Format, outputPath)
	return args
}

func (j *Job) Run(ctx context.Context, ctrl *Controller, progress chan<- Progress, stderrLog io.Writer) error {
	if err := os.MkdirAll(j.OutputDir, 0755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	out := j.partialPath()
	_ = os.Remove(out)

	args := buildArgs(j, out)
	if stderrLog != nil {
		fmt.Fprintf(stderrLog, "\n----- ffmpeg: %s -----\n+ ffmpeg %s\n",
			j.Source, strings.Join(args, " "))
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start ffmpeg: %w", err)
	}
	log.Printf("ffmpeg started pid=%d", cmd.Process.Pid)
	ctrl.set(cmd)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if stderrLog != nil {
			_, _ = io.Copy(stderrLog, stderr)
		} else {
			_, _ = io.Copy(io.Discard, stderr)
		}
	}()

	go func() {
		defer wg.Done()
		parseProgress(stdout, progress)
	}()

	waitErr := cmd.Wait()
	wg.Wait()
	ctrl.clear()

	if waitErr != nil {
		_ = os.Remove(out)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg: %w", waitErr)
	}

	if err := os.Rename(out, j.OutputPath()); err != nil {
		_ = os.Remove(out)
		return fmt.Errorf("finalize output: %w", err)
	}

	return nil
}

func containsFlag(args []string, flag string) bool {
	return slices.Contains(args, flag)
}

func parseProgress(r io.Reader, ch chan<- Progress) {
	scanner := bufio.NewScanner(r)
	cur := Progress{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "out_time_ms", "out_time_us":
			// ffmpeg emits microseconds for both keys despite the name.
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				cur.OutTimeMs = n / 1000
			}
		case "speed":
			s := strings.TrimSuffix(v, "x")
			if s == "N/A" {
				cur.Speed = 0
			} else if f, err := strconv.ParseFloat(s, 64); err == nil {
				cur.Speed = f
			}
		case "bitrate":
			cur.Bitrate = v
		case "frame":
			if n, err := strconv.ParseInt(v, 10, 64); err == nil {
				cur.FrameNum = n
			}
		case "progress":
			if v == "end" {
				cur.Done = true
			}
			select {
			case ch <- cur:
			default:
			}
			if cur.Done {
				return
			}
		}
	}
}
