package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"sway-screenshot/internal/config"
	"sway-screenshot/internal/daemon"
	"sway-screenshot/pkg/protocol"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Parse command-line arguments
	args := os.Args[1:]
	if len(args) == 0 {
		printHelp()
		return nil
	}

	// Parse flags
	delay := 0
	useCurrentScreen := false
	follow := false
	i := 0

	for i < len(args) {
		if args[i] == "-h" || args[i] == "--help" {
			printHelp()
			return nil
		} else if args[i] == "-c" {
			useCurrentScreen = true
			i++
		} else if args[i] == "-t" {
			if i+1 >= len(args) {
				return fmt.Errorf("missing value for -t flag")
			}
			fmt.Sscanf(args[i+1], "%d", &delay)
			i += 2
		} else if args[i] == "-follow" {
			follow = true
			i++
		} else {
			break
		}
	}

	if i >= len(args) {
		printHelp()
		return fmt.Errorf("no command specified")
	}

	action := args[i]

	// Special case: daemon mode
	if action == "daemon" {
		d := daemon.New(cfg)
		return d.Start()
	}

	// For waybar-status, we need to output JSON directly
	if action == "waybar-status" {
		return handleWaybarStatus(cfg, follow)
	}

	// Ensure daemon is running
	if !isDaemonRunning(cfg.SocketPath) {
		if err := startDaemon(cfg); err != nil {
			return fmt.Errorf("failed to start daemon: %w", err)
		}

		// Wait for daemon to be ready
		for i := 0; i < 10; i++ {
			if isDaemonRunning(cfg.SocketPath) {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}

		if !isDaemonRunning(cfg.SocketPath) {
			return fmt.Errorf("daemon failed to start")
		}
	}

	// Send command to daemon
	req := protocol.Request{
		Command: "execute",
		Action:  action,
		Options: map[string]interface{}{
			"delay":              delay,
			"use_current_screen": useCurrentScreen,
		},
	}

	resp, err := sendRequest(cfg.SocketPath, req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("command failed: %s", resp.Message)
	}

	return nil
}

func isDaemonRunning(socketPath string) bool {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func startDaemon(cfg *config.Config) error {
	// Get the current executable path
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, "daemon")
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		return err
	}

	// Detach from parent
	cmd.Process.Release()

	return nil
}

func sendRequest(socketPath string, req protocol.Request) (*protocol.Response, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Set timeout
	conn.SetDeadline(time.Now().Add(30 * time.Second))

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	if err := encoder.Encode(req); err != nil {
		return nil, err
	}

	var resp protocol.Response
	if err := decoder.Decode(&resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

func handleWaybarStatus(cfg *config.Config, follow bool) error {
	if follow {
		return followWaybarStatus(cfg)
	}
	return outputCurrentStatus(cfg)
}

func outputCurrentStatus(cfg *config.Config) error {
	status := getWaybarStatus(cfg)
	return json.NewEncoder(os.Stdout).Encode(status)
}

func getWaybarStatus(cfg *config.Config) *protocol.WaybarStatus {
	if !isDaemonRunning(cfg.SocketPath) {
		// Daemon not running, return idle status
		return &protocol.WaybarStatus{
			Text:    "󰕧",
			Tooltip: "Ready for screenshot/recording",
			Class:   "idle",
			Alt:     "idle",
		}
	}

	req := protocol.Request{
		Command: "execute",
		Action:  "waybar-status",
	}

	resp, err := sendRequest(cfg.SocketPath, req)
	if err != nil {
		// Fallback to idle status on error
		return &protocol.WaybarStatus{
			Text:    "󰕧",
			Tooltip: "Ready for screenshot/recording",
			Class:   "idle",
			Alt:     "idle",
		}
	}

	// Parse the waybar status from response message
	var status protocol.WaybarStatus
	if err := json.Unmarshal([]byte(resp.Message), &status); err != nil {
		// Fallback to idle status on parse error
		return &protocol.WaybarStatus{
			Text:    "󰕧",
			Tooltip: "Ready for screenshot/recording",
			Class:   "idle",
			Alt:     "idle",
		}
	}

	return &status
}

func followWaybarStatus(cfg *config.Config) error {
	var previousStatus *protocol.WaybarStatus
	ticker := time.NewTicker(cfg.WaybarPollInterval)
	defer ticker.Stop()

	// Signal handling for Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM, syscall.SIGINT)

	// Output initial status immediately
	currentStatus := getWaybarStatus(cfg)
	if err := json.NewEncoder(os.Stdout).Encode(currentStatus); err != nil {
		return err
	}
	previousStatus = currentStatus

	for {
		select {
		case <-ticker.C:
			currentStatus := getWaybarStatus(cfg)
			if !statusEqual(previousStatus, currentStatus) {
				if err := json.NewEncoder(os.Stdout).Encode(currentStatus); err != nil {
					return err
				}
				previousStatus = currentStatus
			}
		case <-sigChan:
			return nil
		}
	}
}

func statusEqual(a, b *protocol.WaybarStatus) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Text == b.Text &&
		a.Tooltip == b.Tooltip &&
		a.Class == b.Class &&
		a.Alt == b.Alt
}

func printHelp() {
	help := `Recording and screenshot utility for sway

Usage:
  sway-screenshot [-t seconds] <command>

Options:
  -h, --help        Show this help
  -c                Use current focused screen (skip selection)
  -t <seconds>      Delay capture or recording

Commands:
  daemon                   Run in daemon mode (auto-started if needed)
  waybar-status [-follow]  Output waybar status (JSON)
                           -follow: Continuously monitor and output on state change
                           Poll interval: SWAY_SCREENSHOT_WAYBAR_POLL_INTERVAL (default: 1s)

  obs-toggle-recording     Toggle OBS recording
  obs-toggle-pause         Toggle OBS pause state

  current-window-clipboard Capture focused window to clipboard
  current-window-file      Capture focused window to file
  current-screen-clipboard Capture focused screen to clipboard

  selection-file           Capture selection to file (interactive actions)
  selection-edit           Capture selection and open editor
  selection-clipboard      Capture selection to clipboard (optional save/edit)

  movie-selection          Record video of selection
  movie-screen             Record video of screen
  movie-current-window     Record video of focused window

  stop-recording           Stop wf-recorder and convert to mp4
  pause-recording          Pause/resume current recording
`
	fmt.Print(help)
}
