package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

const fifoPath = "/tmp/streampipe.fifo"

type entry struct {
	ID   string
	File string
}

type streamManager struct {
	mu      sync.RWMutex
	running bool
	ctx     context.Context
	cancel  context.CancelFunc

	queue       []entry
	queueNotify chan struct{}

	currentCtx    context.Context
	currentCancel context.CancelFunc
	currentEntry  *entry
}

func newStreamManager() (*streamManager, error) {
	return &streamManager{
		mu:          sync.RWMutex{},
		queue:       make([]entry, 0),
		queueNotify: make(chan struct{}, 1),
	}, nil
}

func (s *streamManager) run(ctx context.Context, dest string) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		log.Printf("StreamManager is already running")
		return errors.New("already running")
	}
	s.running = true
	s.mu.Unlock()

	_ = os.Remove(fifoPath)
	if err := syscall.Mkfifo(fifoPath, 0o0644); err != nil {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		return fmt.Errorf("failed to create fifo: %w", err)
	}

	log.Println("StreamManager started")

	s.ctx, s.cancel = context.WithCancel(ctx)
	eg, _ := errgroup.WithContext(s.ctx)

	eg.Go(func() error {
		log.Printf("Starting RTMP stream reader to %s", dest)
		if err := readFromFIFO(s.ctx, fifoPath, dest); err != nil {
			return fmt.Errorf("failed to read from fifo: %w", err)
		}
		return nil
	})

	eg.Go(func() error {
		for {
			select {
			case <-s.ctx.Done():
				log.Printf("Queue processor context cancelled")
				return nil
			case <-s.queueNotify:
				s.mu.Lock()
				if len(s.queue) == 0 {
					s.mu.Unlock()
					continue
				}
				entry := s.queue[0]
				s.queue = s.queue[1:]
				s.currentEntry = &entry
				s.currentCtx, s.currentCancel = context.WithCancel(s.ctx)
				s.mu.Unlock()

				log.Printf("Processing file: %s (ID: %s)", entry.File, entry.ID)
				if err := writeToFIFO(s.currentCtx, entry.File); err != nil {
					if errors.Is(err, context.Canceled) {
						log.Printf("Processing of %s was cancelled (ID: %s)", entry.File, entry.ID)
					} else {
						log.Printf("Failed to write %s to fifo: %v", entry.File, err)
					}
					s.mu.Lock()
					s.currentEntry = nil
					s.currentCancel = nil
					s.mu.Unlock()
					continue
				}

				log.Printf("Successfully wrote %s to fifo", entry.File)
				s.mu.Lock()
				s.currentEntry = nil
				s.currentCancel = nil
				s.mu.Unlock()
			}
		}
	})

	err := eg.Wait()

	s.mu.Lock()
	s.running = false
	s.currentEntry = nil
	s.currentCancel = nil
	s.mu.Unlock()

	log.Printf("StreamManager stopped (running=%t, err=%v)", s.running, err)
	return err
}

func (s *streamManager) enqueue(file string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := fmt.Sprintf("%d", time.Now().UnixNano())
	entry := entry{ID: id, File: file}
	s.queue = append(s.queue, entry)

	select {
	case s.queueNotify <- struct{}{}:
	default:
	}

	return id
}

func (s *streamManager) dequeue(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, entry := range s.queue {
		if entry.ID == id {
			s.queue = slices.Delete(s.queue, i, i+1)
			return true
		}
	}
	return false
}

func (s *streamManager) getQueue() []entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]entry, len(s.queue))
	copy(result, s.queue)
	return result
}

func (s *streamManager) status() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := map[string]any{
		"running":     s.running,
		"queueLength": len(s.queue),
	}

	if s.currentEntry != nil {
		status["playing"] = map[string]string{
			"id":   s.currentEntry.ID,
			"file": s.currentEntry.File,
		}
	}

	return status
}

func (s *streamManager) skip() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentCancel != nil {
		s.currentCancel()
		return true
	}
	return false
}

func (s *streamManager) stop() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil && s.running {
		log.Printf("Stopping stream manager")
		s.cancel()
		return true
	}
	return false
}

func writeToFIFO(ctx context.Context, source string) error {
	fifo, err := os.OpenFile(fifoPath, os.O_WRONLY, os.ModeNamedPipe)
	if err != nil {
		return err
	}
	args := []string{
		"-hide_banner",
		"-i", source,
		"-c", "copy",
		"-f", "mpegts",
		"pipe:1",
	}

	log.Printf("Running ffmpeg write command: ffmpeg %s", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdout = fifo
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func readFromFIFO(ctx context.Context, fifo string, dest string) error {
	args := []string{
		"-hide_banner",
		"-loglevel", "warning",
		"-re",
		"-i", fifo,
		"-c", "copy",
		"-f", "flv",
		dest,
	}

	log.Printf("Running ffmpeg read command: ffmpeg %s", strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func main() {
	addr := flag.String("addr", ":8080", "server address")
	dest := flag.String("dest", "", "RTMP destination server URL")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sm, err := newStreamManager()
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		go func() {
			if err := sm.run(ctx, *dest); err != nil {
				log.Printf("Stream manager error: %v", err)
			}
		}()

		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "StreamManager started")
	})

	mux.HandleFunc("/enqueue", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		file := r.URL.Query().Get("file")
		if file == "" {
			http.Error(w, "Missing file parameter", http.StatusBadRequest)
			return
		}
		file, err := filepath.Abs(file)
		if err != nil {
			log.Printf("Failed to get absolute path for %s: %v", file, err)
			http.Error(w, "Unable to find file", http.StatusBadRequest)
			return
		}

		id := sm.enqueue(file)
		log.Printf("File %s added to queue with ID %s", file, id)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":   id,
			"file": file,
		})
	})

	mux.HandleFunc("/queue", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		queue := sm.getQueue()
		status := sm.status()

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status": status,
			"queue":  queue,
		})
	})

	mux.HandleFunc("/queue/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		id := strings.TrimPrefix(r.URL.Path, "/queue/")
		if id == "" {
			http.Error(w, "Missing queue entry ID", http.StatusBadRequest)
			return
		}

		if sm.dequeue(id) {
			log.Printf("Queue entry %s removed", id)
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "Queue entry %s removed", id)
		} else {
			http.Error(w, "Queue entry not found", http.StatusNotFound)
		}
	})

	mux.HandleFunc("/skip", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if sm.skip() {
			log.Println("Current file processing was skipped")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "Current file skipped")
		} else {
			http.Error(w, "No file currently being processed", http.StatusBadRequest)
		}
	})

	mux.HandleFunc("/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if sm.stop() {
			log.Println("Stream manager stopped")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "Stream manager stopped")
		} else {
			http.Error(w, "Stream manager not running", http.StatusBadRequest)
		}
	})

	srvr := &http.Server{
		Addr:    *addr,
		Handler: mux,
	}

	errC := make(chan error, 1)
	go func() {
		log.Println("Listening on " + *addr)
		if err := srvr.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errC <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Println("Shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srvr.Shutdown(ctx); err != nil {
			log.Fatal(err)
		}
	case err := <-errC:
		if err != nil {
			log.Fatal(err)
		}
	}
}
