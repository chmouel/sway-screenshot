package commands

import (
	"context"
	"fmt"
	"strings"
	"sway-screenshot/internal/config"
	"sway-screenshot/internal/external"
	"sway-screenshot/internal/notify"
	"sway-screenshot/internal/state"
	"time"
)

type OBSHandler struct {
	cfg   *config.Config
	state *state.State
}

func NewOBSHandler(cfg *config.Config, st *state.State) *OBSHandler {
	return &OBSHandler{
		cfg:   cfg,
		state: st,
	}
}

func (h *OBSHandler) ToggleRecording(ctx context.Context) error {
	status, err := external.OBSCli(ctx, "recording", "status")
	if err != nil {
		notify.Send(2000, h.cfg.ScreenshotIcon, "Failed to get OBS status")
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
	notify.Send(2000, h.cfg.RecordingStopIcon, "Recording has stopped")

	h.state.SetOBSState(false, false)
	return nil
}

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
		notify.Send(2000, h.cfg.RecordingPauseIcon, "Recording paused")
		h.state.SetOBSState(true, true)
	} else {
		notify.Send(2000, h.cfg.RecordingStartIcon, "Recording resumed")
		h.state.SetOBSState(true, false)
	}

	return nil
}
