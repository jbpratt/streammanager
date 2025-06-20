package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/jbpratt/streammanager/internal/streammanager"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Server struct {
	sm        *streammanager.StreamManager
	logger    *zap.Logger
	rtmpAddr  string
	webrtcSrv WebRTCStatusProvider
	fileDir   string           // Directory to serve files from
	logLevel  *zap.AtomicLevel // Atomic log level for runtime changes
}

type WebRTCStatusProvider interface {
	GetStatus() map[string]any
}

func New(logger *zap.Logger, rtmpAddr string, logLevel *zap.AtomicLevel, fifoPath string) (*Server, error) {
	sm, err := streammanager.New(logger, fifoPath)
	if err != nil {
		return nil, err
	}

	// Default to current directory, can be configured later
	fileDir, err := os.Getwd()
	if err != nil {
		fileDir = "."
	}

	return &Server{
		sm:       sm,
		logger:   logger,
		rtmpAddr: rtmpAddr,
		fileDir:  fileDir,
		logLevel: logLevel,
	}, nil
}

func (s *Server) SetWebRTCServer(webrtcSrv WebRTCStatusProvider) {
	s.webrtcSrv = webrtcSrv
}

func (s *Server) SetFileDirectory(dir string) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("invalid directory path: %w", err)
	}

	if _, err := os.Stat(absDir); os.IsNotExist(err) {
		return fmt.Errorf("directory does not exist: %s", absDir)
	}

	s.fileDir = absDir
	s.logger.Info("File directory set", zap.String("directory", absDir))
	return nil
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
	mux.HandleFunc("/webrtc/status", s.logMiddleware(s.handleWebRTCStatus))
	mux.HandleFunc("/files", s.logMiddleware(s.handleListFiles))
	mux.HandleFunc("/files/", s.logMiddleware(s.handleServeFile))
	mux.HandleFunc("/log-level", s.logMiddleware(s.handleLogLevel))
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
		File           string                        `json:"file"`
		Overlay        streammanager.OverlaySettings `json:"overlay"`
		StartTimestamp string                        `json:"startTimestamp,omitempty"` // Optional start timestamp
		SubtitleFile   string                        `json:"subtitleFile,omitempty"`   // Optional subtitle file
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

	var file string
	var err error

	// Check if the file path is absolute or relative
	if filepath.IsAbs(req.File) {
		// If absolute, use as-is (for local file uploads)
		file = req.File
	} else {
		// If relative, resolve against the configured file directory (for server files)
		file = filepath.Join(s.fileDir, req.File)
	}

	// Get absolute path and validate file exists
	file, err = filepath.Abs(file)
	if err != nil {
		s.logger.Error("Failed to get absolute path for file",
			zap.String("file", req.File),
			zap.String("resolved", file),
			zap.Error(err))
		http.Error(w, "Unable to resolve file path", http.StatusBadRequest)
		return
	}

	// Validate that the file exists
	if _, err := os.Stat(file); os.IsNotExist(err) {
		s.logger.Error("File does not exist",
			zap.String("file", file),
			zap.String("original", req.File))
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	id := s.sm.Enqueue(file, req.Overlay, req.StartTimestamp, req.SubtitleFile)
	s.logger.Info("File added to queue",
		zap.String("file", file),
		zap.String("id", id),
		zap.String("startTimestamp", req.StartTimestamp),
		zap.String("subtitleFile", req.SubtitleFile),
		zap.Any("overlay", req.Overlay))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"id":   id,
		"file": file,
	}); err != nil {
		s.logger.Error("Failed to encode response", zap.Error(err))
	}
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
	if err := json.NewEncoder(w).Encode(map[string]any{
		"status": status,
		"queue":  queue,
	}); err != nil {
		s.logger.Error("Failed to encode queue response", zap.Error(err))
	}
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
	if err := json.NewEncoder(w).Encode(map[string]any{
		"hasProgress": hasProgress,
		"progress":    progress,
	}); err != nil {
		s.logger.Error("Failed to encode progress response", zap.Error(err))
	}
}

func (s *Server) handleWebRTCStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.logger.Warn("Invalid method for /webrtc/status endpoint", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := make(map[string]any)
	if s.webrtcSrv != nil {
		status = s.webrtcSrv.GetStatus()
	} else {
		status["error"] = "WebRTC server not initialized"
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		s.logger.Error("Failed to encode WebRTC status response", zap.Error(err))
	}
}

// FileInfo represents a file entry for the API
type FileInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
	IsDir   bool   `json:"isDir"`
}

// isSecurePath validates that the requested path is safe and within allowed directories
func (s *Server) isSecurePath(requestedPath string) (string, bool) {
	// Clean the path to prevent directory traversal
	cleanPath := filepath.Clean(requestedPath)

	// Prevent access to parent directories
	if strings.Contains(cleanPath, "..") {
		return "", false
	}

	// Build the full path
	fullPath := filepath.Join(s.fileDir, cleanPath)

	// Ensure the resolved path is still within our allowed directory
	absFileDir, err := filepath.Abs(s.fileDir)
	if err != nil {
		return "", false
	}

	absFullPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", false
	}

	if !strings.HasPrefix(absFullPath, absFileDir) {
		return "", false
	}

	return absFullPath, true
}

// handleListFiles lists files in a directory
func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.logger.Warn("Invalid method for /files endpoint", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get path parameter
	dirPath := r.URL.Query().Get("path")
	if dirPath == "" {
		dirPath = "."
	}

	// Validate path security
	safePath, isSafe := s.isSecurePath(dirPath)
	if !isSafe {
		s.logger.Warn("Unsafe path access attempted", zap.String("path", dirPath))
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Read directory
	entries, err := os.ReadDir(safePath)
	if err != nil {
		s.logger.Error("Failed to read directory", zap.String("path", safePath), zap.Error(err))
		http.Error(w, "Directory not found", http.StatusNotFound)
		return
	}

	// Build response
	var files []FileInfo
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Only include video files, subtitle files, and directories
		if !entry.IsDir() && !isVideoFile(entry.Name()) && !isSubtitleFile(entry.Name()) {
			continue
		}

		files = append(files, FileInfo{
			Name:    entry.Name(),
			Path:    filepath.Join(dirPath, entry.Name()),
			Size:    info.Size(),
			ModTime: info.ModTime().Format(time.RFC3339),
			IsDir:   entry.IsDir(),
		})
	}

	s.logger.Debug("Listed files", zap.String("path", safePath), zap.Int("count", len(files)))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"path":  dirPath,
		"files": files,
	}); err != nil {
		s.logger.Error("Failed to encode files response", zap.Error(err))
	}
}

// handleServeFile serves a specific file
func (s *Server) handleServeFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.logger.Warn("Invalid method for /files/ endpoint", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract file path from URL
	filePath := strings.TrimPrefix(r.URL.Path, "/files/")
	if filePath == "" {
		http.Error(w, "File path required", http.StatusBadRequest)
		return
	}

	// Validate path security
	safePath, isSafe := s.isSecurePath(filePath)
	if !isSafe {
		s.logger.Warn("Unsafe file access attempted", zap.String("path", filePath))
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Check if file exists and is not a directory
	info, err := os.Stat(safePath)
	if err != nil {
		s.logger.Error("File not found", zap.String("path", safePath), zap.Error(err))
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	if info.IsDir() {
		http.Error(w, "Path is a directory", http.StatusBadRequest)
		return
	}

	// Only serve video files and subtitle files
	if !isVideoFile(safePath) && !isSubtitleFile(safePath) {
		s.logger.Warn("Non-video/subtitle file access attempted", zap.String("path", safePath))
		http.Error(w, "Only video and subtitle files can be served", http.StatusForbidden)
		return
	}

	s.logger.Debug("Serving file", zap.String("path", safePath))

	// Serve the file
	http.ServeFile(w, r, safePath)
}

// isVideoFile checks if a file is a video file based on extension
func isVideoFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	videoExtensions := []string{
		".mp4", ".avi", ".mkv", ".mov", ".wmv", ".flv", ".webm", ".m4v",
		".mpg", ".mpeg",
	}

	return slices.Contains(videoExtensions, ext)
}

func isSubtitleFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	subtitleExtensions := []string{
		".srt", ".vtt", ".ass", ".ssa", ".sub", ".sbv",
	}

	return slices.Contains(subtitleExtensions, ext)
}

// handleLogLevel handles GET and POST requests for application log level
func (s *Server) handleLogLevel(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetLogLevel(w, r)
	case http.MethodPost:
		s.handleSetLogLevel(w, r)
	default:
		s.logger.Warn("Invalid method for /log-level endpoint", zap.String("method", r.Method))
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleGetLogLevel returns the current application log level
func (s *Server) handleGetLogLevel(w http.ResponseWriter, r *http.Request) {
	if s.logLevel == nil {
		http.Error(w, "Log level not available", http.StatusInternalServerError)
		return
	}

	currentLevel := s.logLevel.Level()

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"level": currentLevel.String(),
	}); err != nil {
		s.logger.Error("Failed to encode log level response", zap.Error(err))
	}
}

// handleSetLogLevel sets the application log level
func (s *Server) handleSetLogLevel(w http.ResponseWriter, r *http.Request) {
	if s.logLevel == nil {
		http.Error(w, "Log level not available", http.StatusInternalServerError)
		return
	}

	var req struct {
		Level string `json:"level"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.logger.Error("Failed to decode log level request", zap.Error(err))
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Level == "" {
		http.Error(w, "Missing level parameter", http.StatusBadRequest)
		return
	}

	var level zapcore.Level
	if err := level.UnmarshalText([]byte(strings.ToLower(req.Level))); err != nil {
		s.logger.Error("Invalid log level", zap.String("level", req.Level), zap.Error(err))
		http.Error(w, "Invalid log level: "+err.Error(), http.StatusBadRequest)
		return
	}

	oldLevel := s.logLevel.Level()
	s.logLevel.SetLevel(level)

	s.logger.Info("Application log level changed",
		zap.String("old_level", oldLevel.String()),
		zap.String("new_level", level.String()))

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]string{
		"level":     level.String(),
		"old_level": oldLevel.String(),
	}); err != nil {
		s.logger.Error("Failed to encode log level response", zap.Error(err))
	}
}
