package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"sonos-linux/audio"
	"sonos-linux/sonos"
	"sonos-linux/stream"
)

var requiredPackages = map[string]string{
	"pactl":  "pulseaudio-utils",
	"ffmpeg": "ffmpeg",
}

func ensureDeps() {
	var missing []string
	for bin, pkg := range requiredPackages {
		if _, err := exec.LookPath(bin); err != nil {
			missing = append(missing, pkg)
		}
	}
	if len(missing) == 0 {
		return
	}

	log.Fatalf("Missing dependencies. Install with:\n  sudo apt install %s", strings.Join(missing, " "))
}

type deviceStream struct {
	device    *sonos.Device
	sinkName  string
	moduleIdx int
	ffmpegCmd *exec.Cmd
	player    *sonos.Player
}

func main() {
	scanOnly := len(os.Args) > 1 && os.Args[1] == "--scan-devices"

	if scanOnly {
		runScan()
		return
	}

	ensureDeps()
	run()
}

func runScan() {
	fmt.Println("Discovering Sonos devices...")
	devices, err := sonos.Discover(5 * time.Second)
	if err != nil {
		log.Fatalf("Discovery failed: %v", err)
	}
	if len(devices) == 0 {
		log.Fatal("No Sonos devices found")
	}

	fmt.Printf("\nFound %d device(s):\n\n", len(devices))
	for _, d := range devices {
		printDevice(d)
	}
}

func printDevice(d *sonos.Device) {
	canPlay := "yes"
	if !d.CanPlay() {
		canPlay = "no"
	}
	coord := ""
	if d.IsCoord {
		coord = " (coordinator)"
	}
	fmt.Printf("  %s\n", d.Name)
	fmt.Printf("    IP:        %s\n", d.IP)
	fmt.Printf("    Model:     %s (%s)\n", d.ModelName, d.ModelNumber)
	fmt.Printf("    UUID:      %s\n", d.UUID)
	fmt.Printf("    Serial:    %s\n", d.SerialNumber)
	fmt.Printf("    Software:  %s\n", d.SoftwareVersion)
	fmt.Printf("    Hardware:  %s\n", d.HardwareVersion)
	fmt.Printf("    Group:     %s%s\n", d.GroupID, coord)
	fmt.Printf("    Invisible: %v\n", d.Invisible)
	fmt.Printf("    Can play:  %s\n", canPlay)
	fmt.Println()
}

func run() {
	// Discover devices
	fmt.Println("Discovering Sonos devices...")
	devices, err := sonos.Discover(5 * time.Second)
	if err != nil {
		log.Fatalf("Discovery failed: %v", err)
	}
	if len(devices) == 0 {
		log.Fatal("No Sonos devices found")
	}

	fmt.Printf("Found %d device(s)\n", len(devices))
	for _, d := range devices {
		printDevice(d)
	}

	// Filter to playable devices
	var playable []*sonos.Device
	for _, d := range devices {
		if d.CanPlay() {
			playable = append(playable, d)
		}
	}
	if len(playable) == 0 {
		log.Fatal("No playable Sonos devices found")
	}

	// Clean up orphaned sinks
	audio.CleanOrphanedSinks()

	// Determine local IP
	localIP, err := localIPFor(playable[0].IP)
	if err != nil {
		log.Fatalf("Failed to determine local IP: %v", err)
	}

	// Start HTTP server
	srv := stream.NewServer()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var streams []deviceStream

	// Set up a sink + encoder + stream for each playable device
	for _, dev := range playable {
		sanitized := audio.SanitizeName(dev.Name)
		sinkName := audio.SinkPrefix + sanitized
		description := "Sonos - " + dev.Name
		streamPath := "/stream/" + sanitized + ".mp3"

		moduleIdx, err := audio.CreateSink(sinkName, description)
		if err != nil {
			log.Printf("Failed to create sink for %s: %v", dev.Name, err)
			continue
		}

		broadcaster := stream.NewBroadcaster(description)
		srv.AddStream(streamPath, broadcaster)

		ffmpegCmd, stdout, err := audio.StartEncoder(ctx, sinkName)
		if err != nil {
			log.Printf("Failed to start encoder for %s: %v", dev.Name, err)
			audio.DestroySink(moduleIdx)
			continue
		}

		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := stdout.Read(buf)
				if n > 0 {
					broadcaster.Write(buf[:n])
				}
				if err != nil {
					return
				}
			}
		}()

		streams = append(streams, deviceStream{
			device:    dev,
			sinkName:  sinkName,
			moduleIdx: moduleIdx,
			ffmpegCmd: ffmpegCmd,
			player:    sonos.NewPlayer(dev),
		})

		fmt.Printf("  Sonos - %-20s  sink: %s\n", dev.Name, sinkName)
	}

	if len(streams) == 0 {
		log.Fatal("Failed to set up any device streams")
	}

	port, err := srv.Start()
	if err != nil {
		log.Fatalf("Failed to start stream server: %v", err)
	}

	time.Sleep(time.Second)

	// Tell each Sonos to play its stream
	for _, s := range streams {
		sanitized := audio.SanitizeName(s.device.Name)
		streamURL := fmt.Sprintf("http://%s:%d/stream/%s.mp3", localIP, port, sanitized)

		fmt.Printf("  %s → %s\n", s.device.Name, streamURL)

		s.player.Stop()

		if err := s.player.SetAVTransportURI(streamURL); err != nil {
			log.Printf("Failed to set URI on %s: %v", s.device.Name, err)
			continue
		}
		if err := s.player.Play(); err != nil {
			log.Printf("Failed to start playback on %s: %v", s.device.Name, err)
			continue
		}
	}

	// Start volume sync and activity monitor for each stream
	for _, s := range streams {
		go syncVolume(ctx, s)
		go monitorActivity(ctx, s)
	}

	// Start system tray
	tray := startTray(devices, streams)

	fmt.Println("\nRoute your audio to any 'Sonos - <name>' sink in your sound settings.")
	fmt.Println("Press Ctrl+C or use tray Quit to stop.")

	// Wait for quit signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigCh:
	case <-tray.quit:
	}

	// Cleanup
	fmt.Println("\nStopping...")
	tray.stop()
	for _, s := range streams {
		s.player.Stop()
	}
	cancel()
	for _, s := range streams {
		s.ffmpegCmd.Wait()
		audio.DestroySink(s.moduleIdx)
	}
	srv.Stop()
}

// syncVolume syncs the PulseAudio sink volume to the Sonos device.
// On startup, reads Sonos volume and sets the sink to match.
// Then polls for user changes and forwards them to Sonos.
func syncVolume(ctx context.Context, s deviceStream) {
	// Initialize sink volume from current Sonos volume
	if vol, err := s.player.GetVolume(); err == nil {
		var sonosVol int
		fmt.Sscanf(vol, "%d", &sonosVol)
		sinkVol := sonosVol * 65536 / 100
		audio.SetSinkVolume(s.sinkName, sinkVol)
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	// Start tracking from the current sink volume so we don't
	// send a volume change on the first tick
	lastVolume := audio.GetSinkVolume(s.sinkName)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sinkVol := audio.GetSinkVolume(s.sinkName)
			if sinkVol < 0 || sinkVol == lastVolume {
				continue
			}
			lastVolume = sinkVol

			// Map sink volume (0-65536) to Sonos volume (0-100)
			sonosVol := sinkVol * 100 / 65536
			sonosVol = min(sonosVol, 100)
			s.player.SetVolume(sonosVol)
		}
	}
}

// monitorActivity watches for sink inputs. When no apps are playing to our
// sink, stop Sonos playback. When an app starts playing, resume.
func monitorActivity(ctx context.Context, s deviceStream) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	wasActive := true // assume active on startup since we just called Play

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			active := audio.HasSinkInputs(s.sinkName)
			if active && !wasActive {
				log.Printf("[%s] Audio detected, resuming Sonos", s.device.Name)
				s.player.Play()
			} else if !active && wasActive {
				log.Printf("[%s] No audio, stopping Sonos", s.device.Name)
				s.player.Pause()
			}
			wasActive = active
		}
	}
}

func localIPFor(remoteIP string) (string, error) {
	conn, err := net.Dial("udp", remoteIP+":1400")
	if err != nil {
		return "", err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String(), nil
}
