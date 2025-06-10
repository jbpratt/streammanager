package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jbpratt/streammanager/internal/streammanager"
	"go.uber.org/zap"
)

type progressData struct {
	Frame      int64     `json:"frame"`
	Fps        float64   `json:"fps"`
	Bitrate    string    `json:"bitrate"`
	TotalSize  int64     `json:"total_size"`
	OutTimeUs  int64     `json:"out_time_us"`
	OutTime    string    `json:"out_time"`
	DupFrames  int64     `json:"dup_frames"`
	DropFrames int64     `json:"drop_frames"`
	Speed      string    `json:"speed"`
	Progress   string    `json:"progress"`
	Timestamp  time.Time `json:"timestamp"`
}

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

func parseProgressData(body string) progressData {
	data := progressData{
		Timestamp: time.Now(),
	}

	lines := strings.SplitSeq(body, "\n")
	for line := range lines {
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "frame":
			if v, err := strconv.ParseInt(value, 10, 64); err == nil {
				data.Frame = v
			}
		case "fps":
			if v, err := strconv.ParseFloat(value, 64); err == nil {
				data.Fps = v
			}
		case "bitrate":
			data.Bitrate = value
		case "total_size":
			if v, err := strconv.ParseInt(value, 10, 64); err == nil {
				data.TotalSize = v
			}
		case "out_time_us":
			if v, err := strconv.ParseInt(value, 10, 64); err == nil {
				data.OutTimeUs = v
			}
		case "out_time":
			data.OutTime = value
		case "dup_frames":
			if v, err := strconv.ParseInt(value, 10, 64); err == nil {
				data.DupFrames = v
			}
		case "drop_frames":
			if v, err := strconv.ParseInt(value, 10, 64); err == nil {
				data.DropFrames = v
			}
		case "speed":
			data.Speed = value
		case "progress":
			data.Progress = value
		}
	}

	return data
}

func (s *Server) SetupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/progress", s.handleProgress)
	mux.HandleFunc("/start", s.handleStart)
	mux.HandleFunc("/enqueue", s.handleEnqueue)
	mux.HandleFunc("/queue", s.handleQueue)
	mux.HandleFunc("/dequeue/", s.handleDequeue)
	mux.HandleFunc("/skip", s.handleSkip)
	mux.HandleFunc("/stop", s.handleStop)
}

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	progress := parseProgressData(string(body))

	s.logger.Debug("Progress update",
		zap.Int64("frame", progress.Frame),
		zap.Float64("fps", progress.Fps),
		zap.String("bitrate", progress.Bitrate),
		zap.String("speed", progress.Speed),
		zap.String("status", progress.Progress))

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var cfg streammanager.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if cfg.Destination == "" {
		http.Error(w, "Missing destination parameter", http.StatusBadRequest)
		return
	}

	// Set RTMP address if not provided
	if cfg.RTMPAddr == "" {
		cfg.RTMPAddr = s.rtmpAddr
	}

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
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		File    string                        `json:"file"`
		Overlay streammanager.OverlaySettings `json:"overlay"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.File == "" {
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
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	queue := s.sm.Queue()
	status := s.sm.Status()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"queue":  queue,
	})
}

func (s *Server) handleDequeue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/dequeue/")
	if id == "" {
		http.Error(w, "Missing queue entry id", http.StatusBadRequest)
		return
	}

	if s.sm.Dequeue(id) {
		s.logger.Info("Queue entry removed", zap.String("id", id))
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Queue entry %s removed", id)
	} else {
		http.Error(w, "Queue entry not found", http.StatusNotFound)
	}
}

func (s *Server) handleSkip(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.sm.Skip() {
		s.logger.Info("Current file processing was skipped")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Current file skipped")
	} else {
		http.Error(w, "No file currently being processed", http.StatusBadRequest)
	}
}

func (s *Server) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.sm.Stop() {
		s.logger.Info("Stream manager stopped")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Stream manager stopped")
	} else {
		http.Error(w, "Stream manager not running", http.StatusBadRequest)
	}
}
