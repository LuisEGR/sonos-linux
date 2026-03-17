package audio

import (
	"context"
	"io"
	"os"
	"os/exec"
)

// StartEncoder launches ffmpeg to capture audio from the named sink's monitor
// source and encode it as a continuous MP3 stream.
func StartEncoder(ctx context.Context, sinkName string) (*exec.Cmd, io.ReadCloser, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-loglevel", "error",
		// Low-latency input
		"-fflags", "+nobuffer",
		"-flags", "+low_delay",
		"-probesize", "32",
		"-analyzeduration", "0",
		// PulseAudio capture
		"-f", "pulse",
		"-i", sinkName+".monitor",
		// MP3 encoding - only reliable format for Sonos live streaming
		"-acodec", "libmp3lame",
		"-b:a", "192k",
		"-ar", "44100",
		"-ac", "2",
		"-reservoir", "0",
		"-flush_packets", "1",
		"-f", "mp3",
		"pipe:1",
	)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	return cmd, stdout, nil
}
