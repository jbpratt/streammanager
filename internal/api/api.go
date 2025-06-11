package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/jbpratt/streammanager/internal/streammanager"
	"go.uber.org/zap"
)

type Server struct {
	sm       *streammanager.StreamManager
	logger   *zap.Logger
	rtmpAddr string
}

func New(logger *zap.Logger, rtmpAddr string) (*Server, error) {
	sm, err := streammanager.New(logger)
	if err != nil {
		return nil, err
	}
	return &Server{
		sm:       sm,
		logger:   logger,
		rtmpAddr: rtmpAddr,
	}, nil
}

func (s *Server) StreamManager() *streammanager.StreamManager {
	return s.sm
}

func (s *Server) logMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		s.logger.Debug("Request started",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr),
			zap.String("user_agent", r.UserAgent()),
		)

		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next(wrapped, r)

		duration := time.Since(start)
		s.logger.Debug("Request completed",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Int("status", wrapped.statusCode),
			zap.Duration("duration", duration),
		)
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (s *Server) SetupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/start", s.logMiddleware(s.handleStart))
	mux.HandleFunc("/enqueue", s.logMiddleware(s.handleEnqueue))
	mux.HandleFunc("/queue", s.logMiddleware(s.handleQueue))
	mux.HandleFunc("/dequeue/", s.logMiddleware(s.handleDequeue))
	mux.HandleFunc("/skip", s.logMiddleware(s.handleSkip))
	mux.HandleFunc("/stop", s.logMiddleware(s.handleStop))
	mux.HandleFunc("/progress", s.logMiddleware(s.handleProgress))
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.logger.Warn("Invalid method for /start endpoint", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var cfg streammanager.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		s.logger.Error("Failed to decode JSON request", zap.Error(err))
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if cfg.Destination == "" {
		s.logger.Warn("Missing destination parameter in start request")
		http.Error(w, "Missing destination parameter", http.StatusBadRequest)
		return
	}

	// Set RTMP address if not provided
	if cfg.RTMPAddr == "" {
		cfg.RTMPAddr = s.rtmpAddr
	}

	s.logger.Info("Starting stream manager",
		zap.String("destination", cfg.Destination),
		zap.String("rtmp_addr", cfg.RTMPAddr))

	go func() {
		if err := s.sm.Run(context.Background(), cfg); err != nil {
			s.logger.Info("Stream manager stopped", zap.Error(err))
		}
	}()

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "StreamManager started")
}

func (s *Server) handleEnqueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.logger.Warn("Invalid method for /enqueue endpoint", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		File    string                        `json:"file"`
		Overlay streammanager.OverlaySettings `json:"overlay"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.logger.Error("Failed to decode JSON request for enqueue", zap.Error(err))
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.File == "" {
		s.logger.Warn("Missing file parameter in enqueue request")
		http.Error(w, "Missing file parameter", http.StatusBadRequest)
		return
	}

	file, err := filepath.Abs(req.File)
	if err != nil {
		s.logger.Error("Failed to get absolute path for file",
			zap.String("file", req.File),
			zap.Error(err))
		http.Error(w, "Unable to find file", http.StatusBadRequest)
		return
	}

	id := s.sm.Enqueue(file, req.Overlay)
	s.logger.Info("File added to queue",
		zap.String("file", file),
		zap.String("id", id),
		zap.Any("overlay", req.Overlay))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"id":   id,
		"file": file,
	})
}

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.logger.Warn("Invalid method for /queue endpoint", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queue := s.sm.Queue()
	status := s.sm.Status()

	s.logger.Debug("Queue status requested",
		zap.Any("status", status),
		zap.Int("queue_length", len(queue)))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"queue":  queue,
	})
}

func (s *Server) handleDequeue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		s.logger.Warn("Invalid method for /dequeue endpoint", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/dequeue/")
	if id == "" {
		s.logger.Warn("Missing queue entry id in dequeue request")
		http.Error(w, "Missing queue entry id", http.StatusBadRequest)
		return
	}

	if s.sm.Dequeue(id) {
		s.logger.Info("Queue entry removed", zap.String("id", id))
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Queue entry %s removed", id)
	} else {
		s.logger.Warn("Queue entry not found for dequeue", zap.String("id", id))
		http.Error(w, "Queue entry not found", http.StatusNotFound)
	}
}

func (s *Server) handleSkip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.logger.Warn("Invalid method for /skip endpoint", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.sm.Skip() {
		s.logger.Info("Current file processing was skipped")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Current file skipped")
	} else {
		s.logger.Warn("Skip requested but no file currently being processed")
		http.Error(w, "No file currently being processed", http.StatusBadRequest)
	}
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.logger.Warn("Invalid method for /stop endpoint", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.sm.Stop() {
		s.logger.Info("Stream manager stopped")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Stream manager stopped")
	} else {
		s.logger.Warn("Stop requested but stream manager not running")
		http.Error(w, "Stream manager not running", http.StatusBadRequest)
	}
}

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.logger.Warn("Invalid method for /progress endpoint", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	progress, hasProgress := s.sm.GetLatestProgress()
	
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"hasProgress": hasProgress,
		"progress":    progress,
	})
}
