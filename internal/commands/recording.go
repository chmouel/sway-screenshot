package commands

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"sway-easyshot/internal/config"
	"sway-easyshot/internal/external"
	"sway-easyshot/internal/notify"
	"sway-easyshot/internal/state"
	"sway-easyshot/internal/sway"
)

// RecordingHandler provides methods for video recording operations.
type RecordingHandler struct {
	cfg   *config.Config
	state *state.State
}

// NewRecordingHandler creates a new recording handler instance.
func NewRecordingHandler(cfg *config.Config, st *state.State) *RecordingHandler {
	return &RecordingHandler{
		cfg:   cfg,
		state: st,
	}
}

// MovieSelection records a video of a selected region.
func (h *RecordingHandler) MovieSelection(ctx context.Context, delay int) error {
	if err := notify.CaptureDelay(delay, "movie selection", h.cfg.RecordingStartIcon); err != nil {
		return err
	}

	geom, err := external.Slurp(ctx, "")
	if err != nil || geom == "" {
		return fmt.Errorf("selection cancelled or failed: %w", err)
	}

	sleepWithCountdown(h.state, delay)

	return h.startRecording(ctx, geom, "")
}

// MovieScreen records a video of the screen (or current screen if useCurrentScreen is true).
func (h *RecordingHandler) MovieScreen(ctx context.Context, delay int, useCurrentScreen bool) error {
	output, err := sway.SelectOutput(ctx, useCurrentScreen)
	if err != nil || output == "" {
		return fmt.Errorf("failed to select output: %w", err)
	}

	if err := notify.CaptureDelay(delay, "movie screen", h.cfg.RecordingStartIcon); err != nil {
		return err
	}

	sleepWithCountdown(h.state, delay)

	return h.startRecording(ctx, "", output)
}

// MovieCurrentWindow records a video of the currently focused window.
func (h *RecordingHandler) MovieCurrentWindow(ctx context.Context, delay int) error {
	if err := notify.CaptureDelay(delay, "movie current window", h.cfg.RecordingStartIcon); err != nil {
		return err
	}

	geom, err := sway.GetFocusedWindowGeometry(ctx)
	if err != nil {
		return fmt.Errorf("failed to get window geometry: %w", err)
	}

	sleepWithCountdown(h.state, delay)

	return h.startRecording(ctx, geom, "")
}

func (h *RecordingHandler) startRecording(ctx context.Context, geometry, output string) error {
	base := h.cfg.GenerateRecordingBase()
	file := base + ".avi"

	// Check if file exists, add PID suffix if needed
	if _, err := os.Stat(base + ".mp4"); err == nil {
		file = fmt.Sprintf("%s-%d.avi", base, os.Getpid())
		base = fmt.Sprintf("%s-%d", base, os.Getpid())
	}

	// Save base filename to cache
	if err := os.WriteFile(h.cfg.CacheFile, []byte(base), 0o600); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	// Start wf-recorder
	cmd, err := external.StartWfRecorder(ctx, geometry, output, file)
	if err != nil {
		return fmt.Errorf("failed to start recording: %w", err)
	}

	// Update state
	h.state.SetRecording(true, file, cmd.Process.Pid)

	// Monitor process in background
	go func() {
		_ = cmd.Wait()
		h.state.SetRecording(false, "", 0)
	}()

	return nil
}

// StopRecording stops the current recording and converts it to MP4.
func (h *RecordingHandler) StopRecording(ctx context.Context) error {
	// Kill wf-recorder
	_ = exec.Command("killall", "-s", "SIGINT", "wf-recorder").Run() //nolint:gosec

	// Wait a bit for process to terminate
	time.Sleep(500 * time.Millisecond)

	// Read cache file for base name
	data, err := os.ReadFile(h.cfg.CacheFile)
	if err != nil {
		return fmt.Errorf("failed to read cache file: %w", err)
	}

	base := string(data)
	aviFile := base + ".avi"

	// Check if .avi file exists
	if _, err := os.Stat(aviFile); os.IsNotExist(err) {
		_ = notify.Send(5000, h.cfg.ScreenshotIcon, fmt.Sprintf("Could not find %s", aviFile))
		return fmt.Errorf("recording file not found: %s", aviFile)
	}

	_ = notify.Send(3000, h.cfg.ScreenshotIcon, "Recording finished, converting")

	// Convert to mp4
	mp4File := base + ".mp4"
	if err := external.Ffmpeg(ctx, aviFile, mp4File); err != nil {
		return fmt.Errorf("failed to convert video: %w", err)
	}

	// Clean up
	_ = os.Remove(aviFile)
	_ = os.Remove(h.cfg.CacheFile)

	// Update state
	h.state.SetRecording(false, "", 0)

	_ = notify.Send(5000, h.cfg.RecordingStopIcon, fmt.Sprintf("%s is available", base+".mp4"))

	return nil
}

// PauseRecording pauses or resumes the current recording.
func (h *RecordingHandler) PauseRecording(ctx context.Context) error {
	pid := h.state.GetRecordingPID()
	if pid == 0 {
		return fmt.Errorf("no recording in progress")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find recording process: %w", err)
	}

	// Send SIGUSR1 to pause/resume wf-recorder
	if err := process.Signal(syscall.SIGUSR1); err != nil {
		return fmt.Errorf("failed to pause recording: %w", err)
	}

	// Toggle paused state
	currentState := h.state.GetState()
	newPausedState := !currentState.Paused
	h.state.SetPaused(newPausedState)

	if newPausedState {
		_ = notify.Send(2000, h.cfg.RecordingPauseIcon, "Recording paused")
	} else {
		_ = notify.Send(2000, h.cfg.RecordingStartIcon, "Recording resumed")
	}

	return nil
}

// ToggleRecord toggles recording state: starts if not recording, stops if recording.
func (h *RecordingHandler) ToggleRecord(ctx context.Context, startAction string, delay int, useCurrentScreen bool) error {
	// Check current state
	currentState := h.state.GetState()

	if currentState.Recording {
		// Currently recording, stop it
		return h.StopRecording(ctx)
	}

	// Not recording, validate and start with specified action
	switch startAction {
	case "movie-selection":
		return h.MovieSelection(ctx, delay)

	case "movie-screen":
		return h.MovieScreen(ctx, delay, useCurrentScreen)

	case "movie-current-window":
		return h.MovieCurrentWindow(ctx, delay)

	default:
		return fmt.Errorf("invalid start action: %s (valid: movie-selection, movie-screen, movie-current-window)", startAction)
	}
}
