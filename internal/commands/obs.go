package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"sway-easyshot/internal/config"
	"sway-easyshot/internal/external"
	"sway-easyshot/internal/notify"
	"sway-easyshot/internal/state"
)

// OBSHandler provides methods to interact with OBS.
type OBSHandler struct {
	cfg   *config.Config
	state *state.State
}

// NewOBSHandler creates a new OBS handler instance.
func NewOBSHandler(cfg *config.Config, st *state.State) *OBSHandler {
	return &OBSHandler{
		cfg:   cfg,
		state: st,
	}
}

// ToggleRecording toggles OBS recording state (start/stop).
func (h *OBSHandler) ToggleRecording(ctx context.Context) error {
	status, err := external.OBSCli(ctx, "recording", "status")
	if err != nil {
		_ = notify.Send(2000, h.cfg.ScreenshotIcon, "Failed to get OBS status")
		return fmt.Errorf("failed to get OBS recording status: %w", err)
	}

	if strings.Contains(status, "false") || !strings.Contains(status, "Recording: true") {
		// Start recording
		time.Sleep(1 * time.Second)

		if _, err := external.OBSCli(ctx, "recording", "start"); err != nil {
			return fmt.Errorf("failed to start OBS recording: %w", err)
		}

		h.state.SetOBSState(true, false)
		return nil
	}

	// Stop recording
	if _, err := external.OBSCli(ctx, "recording", "stop"); err != nil {
		return fmt.Errorf("failed to stop OBS recording: %w", err)
	}

	time.Sleep(2 * time.Second)
	_ = notify.Send(2000, h.cfg.RecordingStopIcon, "Recording has stopped")

	h.state.SetOBSState(false, false)
	return nil
}

// TogglePause toggles OBS pause state (paused/resumed).
func (h *OBSHandler) TogglePause(ctx context.Context) error {
	if _, err := external.OBSCli(ctx, "recording", "pause", "toggle"); err != nil {
		return fmt.Errorf("failed to toggle OBS pause: %w", err)
	}

	status, err := external.OBSCli(ctx, "recording", "status")
	if err != nil {
		return fmt.Errorf("failed to get OBS recording status: %w", err)
	}

	isPaused := strings.Contains(status, "Paused: true")

	if isPaused {
		_ = notify.Send(2000, h.cfg.RecordingPauseIcon, "Recording paused")
		h.state.SetOBSState(true, true)
	} else {
		_ = notify.Send(2000, h.cfg.RecordingStartIcon, "Recording resumed")
		h.state.SetOBSState(true, false)
	}

	return nil
}
