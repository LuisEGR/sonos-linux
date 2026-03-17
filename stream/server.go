package stream

import (
	"log"
	"net"
	"net/http"
	"sync"
)

// Broadcaster fans out MP3 data to all connected HTTP clients.
type Broadcaster struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
	name    string
}

func NewBroadcaster(name string) *Broadcaster {
	return &Broadcaster{
		clients: make(map[chan []byte]struct{}),
		name:    name,
	}
}

// Write sends data to all subscribed clients. Implements io.Writer.
func (b *Broadcaster) Write(p []byte) (int, error) {
	data := make([]byte, len(p))
	copy(data, p)

	b.mu.RLock()
	defer b.mu.RUnlock()

	for ch := range b.clients {
		select {
		case ch <- data:
		default:
			// Client too slow, drop this chunk
		}
	}
	return len(p), nil
}

func (b *Broadcaster) subscribe() chan []byte {
	ch := make(chan []byte, 128)
	b.mu.Lock()
	b.clients[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *Broadcaster) unsubscribe(ch chan []byte) {
	b.mu.Lock()
	delete(b.clients, ch)
	b.mu.Unlock()
}

// Server serves MP3 streams over HTTP, one path per Sonos device.
type Server struct {
	mux        *http.ServeMux
	listener   net.Listener
	httpServer *http.Server
}

func NewServer() *Server {
	return &Server{mux: http.NewServeMux()}
}

// AddStream registers a broadcaster at the given path (e.g. "/stream/living_room.mp3").
func (s *Server) AddStream(path string, b *Broadcaster) {
	s.mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		handleStream(w, r, b)
	})
}

// Start begins serving on a random port. Returns the port number.
func (s *Server) Start() (int, error) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		return 0, err
	}
	s.listener = ln
	s.httpServer = &http.Server{Handler: s.mux}

	go func() {
		if err := s.httpServer.Serve(ln); err != http.ErrServerClosed {
			log.Printf("HTTP server error: %v", err)
		}
	}()

	return ln.Addr().(*net.TCPAddr).Port, nil
}

func (s *Server) Stop() {
	if s.httpServer != nil {
		s.httpServer.Close()
	}
}

func handleStream(w http.ResponseWriter, r *http.Request, b *Broadcaster) {
	log.Printf("[stream] %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Connection", "close")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("transferMode.dlna.org", "Streaming")
	w.Header().Set("icy-name", b.name)

	ch := b.subscribe()
	defer b.unsubscribe(ch)

	flusher, canFlush := w.(http.Flusher)

	for {
		select {
		case data, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write(data); err != nil {
				return
			}
			if canFlush {
				flusher.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}
