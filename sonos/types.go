package sonos

import "strings"

// Device represents a Sonos speaker on the network.
type Device struct {
	Name            string
	IP              string
	UUID            string
	GroupID         string
	IsCoord         bool
	Invisible       bool
	ModelName       string
	ModelNumber     string
	SerialNumber    string
	SoftwareVersion string
	HardwareVersion string
}

// CanPlay returns true if this device can play audio independently.
// Filters out subwoofers, bridges, boosts, and invisible/satellite devices.
func (d *Device) CanPlay() bool {
	if d.Invisible {
		return false
	}
	lower := strings.ToLower(d.ModelName)
	for _, skip := range []string{"sub", "bridge", "boost"} {
		if strings.Contains(lower, skip) {
			return false
		}
	}
	return true
}
