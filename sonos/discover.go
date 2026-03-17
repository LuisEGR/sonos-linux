package sonos

import (
	"encoding/xml"
	"fmt"
	"net"
	"strings"
	"syscall"
	"time"
)

const ssdpAddr = "239.255.255.250:1900"
const ssdpSearchTarget = "urn:schemas-upnp-org:device:ZonePlayer:1"

// Discover finds Sonos devices on the local network via SSDP.
func Discover(timeout time.Duration) ([]*Device, error) {
	addr, err := net.ResolveUDPAddr("udp4", ssdpAddr)
	if err != nil {
		return nil, err
	}

	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Set multicast TTL to 4 per UPnP spec
	if raw, err := conn.SyscallConn(); err == nil {
		raw.Control(func(fd uintptr) {
			syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_MULTICAST_TTL, 4)
		})
	}

	msg := fmt.Sprintf(
		"M-SEARCH * HTTP/1.1\r\n"+
			"HOST: %s\r\n"+
			"MAN: \"ssdp:discover\"\r\n"+
			"MX: 2\r\n"+
			"ST: %s\r\n"+
			"\r\n",
		ssdpAddr, ssdpSearchTarget,
	)

	// Send multiple times since UDP is unreliable
	for range 3 {
		conn.WriteToUDP([]byte(msg), addr)
	}

	conn.SetReadDeadline(time.Now().Add(timeout))

	var firstIP string
	seen := make(map[string]bool)
	buf := make([]byte, 4096)

	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			break
		}

		response := string(buf[:n])
		if !strings.Contains(strings.ToUpper(response), "SONOS") {
			continue
		}

		ip := extractIPFromLocation(response)
		if ip != "" && !seen[ip] {
			seen[ip] = true
			if firstIP == "" {
				firstIP = ip
			}
		}
	}

	if firstIP == "" {
		return nil, fmt.Errorf("no Sonos devices found")
	}

	return queryTopology(firstIP)
}

func extractIPFromLocation(response string) string {
	for _, line := range strings.Split(response, "\r\n") {
		upper := strings.ToUpper(strings.TrimSpace(line))
		if strings.HasPrefix(upper, "LOCATION:") {
			loc := strings.TrimSpace(line[len("LOCATION:"):])
			loc = strings.TrimPrefix(loc, "http://")
			loc = strings.TrimPrefix(loc, "https://")
			if idx := strings.Index(loc, ":"); idx > 0 {
				return loc[:idx]
			}
		}
	}
	return ""
}

func queryTopology(ip string) ([]*Device, error) {
	result, err := soapCall(ip,
		"/ZoneGroupTopology/Control",
		"ZoneGroupTopology",
		"GetZoneGroupState",
		[]soapArg{},
	)
	if err != nil {
		return nil, fmt.Errorf("query topology: %w", err)
	}

	state, ok := result["ZoneGroupState"]
	if !ok {
		return nil, fmt.Errorf("no ZoneGroupState in response")
	}

	return parseZoneGroupState(state)
}

type zoneGroupState struct {
	XMLName xml.Name    `xml:"ZoneGroupState"`
	Groups  []zoneGroup `xml:"ZoneGroups>ZoneGroup"`
}

type zoneGroup struct {
	Coordinator string       `xml:"Coordinator,attr"`
	ID          string       `xml:"ID,attr"`
	Members     []zoneMember `xml:"ZoneGroupMember"`
}

type zoneMember struct {
	UUID      string `xml:"UUID,attr"`
	ZoneName  string `xml:"ZoneName,attr"`
	Location  string `xml:"Location,attr"`
	Invisible string `xml:"Invisible,attr"`
}

func parseZoneGroupState(state string) ([]*Device, error) {
	var parsed zoneGroupState
	if err := xml.Unmarshal([]byte(state), &parsed); err != nil {
		return nil, fmt.Errorf("parse zone state: %w", err)
	}

	var devices []*Device
	for _, g := range parsed.Groups {
		for _, m := range g.Members {
			ip := extractIPFromURL(m.Location)
			if ip == "" {
				continue
			}
			dev := &Device{
				Name:      m.ZoneName,
				IP:        ip,
				UUID:      m.UUID,
				GroupID:   g.ID,
				IsCoord:   m.UUID == g.Coordinator,
				Invisible: m.Invisible == "1",
			}
			FetchDeviceInfo(dev)
			devices = append(devices, dev)
		}
	}

	return devices, nil
}

func extractIPFromURL(url string) string {
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "https://")
	if idx := strings.Index(url, ":"); idx > 0 {
		return url[:idx]
	}
	if idx := strings.Index(url, "/"); idx > 0 {
		return url[:idx]
	}
	return url
}
