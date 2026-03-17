package audio

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

const SinkPrefix = "sonos_"

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9]+`)

// SanitizeName converts a device name to a valid PulseAudio sink name suffix.
func SanitizeName(name string) string {
	s := strings.ToLower(name)
	s = nonAlphanumeric.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	return s
}

// CreateSink creates a PipeWire null audio sink with the given internal name and
// user-visible description. Returns the PipeWire node ID for later cleanup.
func CreateSink(sinkName, description string) (int, error) {
	props := fmt.Sprintf(
		`{ factory.name=support.null-audio-sink node.name=%s media.class=Audio/Sink object.linger=true audio.channels=2 audio.position=[FL,FR] node.description="%s" monitor.channel-volumes=true monitor.passthrough=true }`,
		sinkName, description,
	)
	cmd := exec.Command("pw-cli", "create-node", "adapter", props)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("create sink %s: %w: %s", sinkName, err, out)
	}

	// Get the node ID from pw-dump
	nodeID, err := getNodeID(sinkName)
	if err != nil {
		return 0, err
	}

	// Set the monitor source to max so ffmpeg captures at full level
	exec.Command("pactl", "set-source-volume", sinkName+".monitor", "65536").Run()

	return nodeID, nil
}

// DestroySink removes a PipeWire node by ID.
func DestroySink(nodeID int) error {
	return exec.Command("pw-cli", "destroy", fmt.Sprintf("%d", nodeID)).Run()
}

// getNodeID finds the PipeWire node ID for a named sink.
func getNodeID(sinkName string) (int, error) {
	out, err := exec.Command("pw-dump").Output()
	if err != nil {
		return 0, fmt.Errorf("pw-dump: %w", err)
	}

	var nodes []map[string]any
	if err := json.Unmarshal(out, &nodes); err != nil {
		return 0, fmt.Errorf("parse pw-dump: %w", err)
	}

	for _, node := range nodes {
		info, _ := node["info"].(map[string]any)
		if info == nil {
			continue
		}
		props, _ := info["props"].(map[string]any)
		if props == nil {
			continue
		}
		if props["node.name"] == sinkName {
			if id, ok := node["id"].(float64); ok {
				return int(id), nil
			}
		}
	}

	return 0, fmt.Errorf("node %s not found", sinkName)
}

// GetSinkVolume returns the current volume of a PulseAudio sink (0-65536), or -1 on error.
func GetSinkVolume(sinkName string) int {
	out, err := exec.Command("pactl", "get-sink-volume", sinkName).Output()
	if err != nil {
		return -1
	}
	s := strings.TrimSpace(string(out))
	for _, field := range strings.Fields(s) {
		var vol int
		if _, err := fmt.Sscanf(field, "%d", &vol); err == nil && vol >= 0 {
			return vol
		}
	}
	return -1
}

// SetSinkVolume sets the volume of a PulseAudio sink (0-65536).
func SetSinkVolume(sinkName string, volume int) {
	exec.Command("pactl", "set-sink-volume", sinkName, fmt.Sprintf("%d", volume)).Run()
}

// GetSinkIndex returns the PulseAudio index for a named sink, or "" on error.
func GetSinkIndex(sinkName string) string {
	out, err := exec.Command("pactl", "list", "short", "sinks").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == sinkName {
			return fields[0]
		}
	}
	return ""
}

// HasSinkInputs returns true if any applications are currently playing to the named sink.
func HasSinkInputs(sinkName string) bool {
	idx := GetSinkIndex(sinkName)
	if idx == "" {
		return false
	}
	out, err := exec.Command("pactl", "list", "short", "sink-inputs").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == idx {
			return true
		}
	}
	return false
}

// CleanOrphanedSinks removes any leftover sonos_ null sinks from previous runs.
func CleanOrphanedSinks() {
	out, err := exec.Command("pw-dump").Output()
	if err != nil {
		return
	}

	var nodes []map[string]any
	if err := json.Unmarshal(out, &nodes); err != nil {
		return
	}

	for _, node := range nodes {
		info, _ := node["info"].(map[string]any)
		if info == nil {
			continue
		}
		props, _ := info["props"].(map[string]any)
		if props == nil {
			continue
		}
		name, _ := props["node.name"].(string)
		if strings.HasPrefix(name, SinkPrefix) {
			if id, ok := node["id"].(float64); ok {
				exec.Command("pw-cli", "destroy", fmt.Sprintf("%d", int(id))).Run()
			}
		}
	}
}
