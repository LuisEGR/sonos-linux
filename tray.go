package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"sonos-linux/sonos"
)

type trayApp struct {
	cmd  *exec.Cmd
	quit chan struct{}
}

type trayGroup struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Active  bool         `json:"active"`
	Devices []trayDevice `json:"devices"`
}

type trayDevice struct {
	Name            string `json:"name"`
	IP              string `json:"ip"`
	Model           string `json:"model"`
	ModelNumber     string `json:"model_number"`
	UUID            string `json:"uuid"`
	Serial          string `json:"serial"`
	Software        string `json:"software"`
	Hardware        string `json:"hardware"`
	IsCoordinator   bool   `json:"is_coordinator"`
	Invisible       bool   `json:"invisible"`
	CanPlay         bool   `json:"can_play"`
}

func startTray(devices []*sonos.Device, streams []deviceStream) *trayApp {
	t := &trayApp{quit: make(chan struct{})}

	trayScript := findTrayScript()
	if trayScript == "" {
		log.Print("tray.py not found, running without system tray")
		return t
	}

	// Build set of active device UUIDs (ones we're streaming to)
	activeUUIDs := make(map[string]bool)
	for _, s := range streams {
		activeUUIDs[s.device.UUID] = true
	}

	// Group devices by GroupID
	groupMap := make(map[string]*trayGroup)
	var groupOrder []string
	for _, dev := range devices {
		g, exists := groupMap[dev.GroupID]
		if !exists {
			g = &trayGroup{ID: dev.GroupID}
			groupMap[dev.GroupID] = g
			groupOrder = append(groupOrder, dev.GroupID)
		}
		td := trayDevice{
			Name:          dev.Name,
			IP:            dev.IP,
			Model:         dev.ModelName,
			ModelNumber:   dev.ModelNumber,
			UUID:          dev.UUID,
			Serial:        dev.SerialNumber,
			Software:      dev.SoftwareVersion,
			Hardware:      dev.HardwareVersion,
			IsCoordinator: dev.IsCoord,
			Invisible:     dev.Invisible,
			CanPlay:       dev.CanPlay(),
		}
		g.Devices = append(g.Devices, td)
		// Name the group after the coordinator
		if dev.IsCoord {
			g.Name = dev.Name
		}
		// Group is active if any of its devices is being streamed to
		if activeUUIDs[dev.UUID] {
			g.Active = true
		}
	}

	var groups []trayGroup
	for _, id := range groupOrder {
		groups = append(groups, *groupMap[id])
	}

	data, _ := json.Marshal(map[string]any{"groups": groups})

	cmd := exec.Command("python3", trayScript)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		log.Printf("Failed to create tray stdin pipe: %v", err)
		return t
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("Failed to create tray stdout pipe: %v", err)
		return t
	}

	if err := cmd.Start(); err != nil {
		log.Printf("Failed to start tray: %v", err)
		return t
	}
	t.cmd = cmd

	fmt.Fprintf(stdin, "%s\n", data)

	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "QUIT" {
				close(t.quit)
				return
			}
		}
		select {
		case <-t.quit:
		default:
			close(t.quit)
		}
	}()

	return t
}

func (t *trayApp) stop() {
	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Signal(os.Interrupt)
		t.cmd.Wait()
	}
}

func findTrayScript() string {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "tray.py")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if _, err := os.Stat("tray.py"); err == nil {
		return "tray.py"
	}
	return ""
}
