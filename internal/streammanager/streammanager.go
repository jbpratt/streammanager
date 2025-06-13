package streammanager

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type entry struct {
	ID             string          `json:"id"`
	File           string          `json:"file"`
	Overlay        OverlaySettings `json:"overlay"`
	StartTimestamp string          `json:"startTimestamp,omitempty"` // Format: HH:MM:SS or seconds
	SubtitleFile   string          `json:"subtitleFile,omitempty"`   // Path to subtitle file
}

type OverlaySettings struct {
	ShowFilename bool   `json:"showFilename"`
	Position     string `json:"position"`
	FontSize     int    `json:"fontSize"`
}

type Config struct {
	Destination      string `json:"destination"`
	MaxBitrate       string `json:"maxBitrate"`
	Username         string `json:"username"`
	Password         string `json:"password"`
	Encoder          string `json:"encoder"`
	Preset           string `json:"preset"`
	RTMPAddr         string `json:"rtmpAddr"`
	LogLevel         string `json:"logLevel"`
	KeyframeInterval string `json:"keyframeInterval"` // GOP size in frames, e.g. "60"
}

type StreamManager struct {
	config        Config
	mu            sync.RWMutex
	running       bool
	ctx           context.Context
	cancel        context.CancelFunc
	logger        *zap.Logger
	queue         []entry
	queueNotify   chan struct{}
	currentCtx    context.Context
	currentCancel context.CancelFunc
	currentEntry  *entry
	lastError     string
	lastErrorTime time.Time
	progressCh    chan progressData
	fifoPath      string
}

func New(logger *zap.Logger, fifoPath string) (*StreamManager, error) {
	return &StreamManager{
		mu:          sync.RWMutex{},
		logger:      logger,
		queue:       make([]entry, 0),
		queueNotify: make(chan struct{}, 1),
		progressCh:  make(chan progressData, 100),
		fifoPath:    fifoPath,
	}, nil
}

// cleanup resets the StreamManager state and cleans up resources
func (s *StreamManager) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.running = false
	s.currentEntry = nil
	if s.currentCancel != nil {
		s.currentCancel()
		s.currentCancel = nil
	}

	// Reset main context references
	s.ctx = nil
	s.cancel = nil

	_ = os.Remove(s.fifoPath)
}

func (s *StreamManager) Run(ctx context.Context, cfg Config) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return errors.New("already running")
	}
	s.running = true
	s.config = cfg
	s.lastError = ""
	s.lastErrorTime = time.Time{}
	s.mu.Unlock()

	// Ensure cleanup runs on any exit
	defer s.cleanup()

	_ = os.Remove(s.fifoPath)
	if err := syscall.Mkfifo(s.fifoPath, 0o0644); err != nil {
		return fmt.Errorf("failed to create fifo: %w", err)
	}

	s.logger.Info("StreamManager started")

	eg, ctx := errgroup.WithContext(ctx)
	s.ctx, s.cancel = context.WithCancel(ctx)

	eg.Go(func() error {
		s.logger.Debug("Streaming FIFO reader", zap.String("destination", s.config.Destination))
		if err := s.readFromFIFO(s.ctx, s.fifoPath); err != nil {
			if errors.Is(err, context.Canceled) {
				s.logger.Debug("FIFO reader cancelled")
				return nil
			}
			s.setError(fmt.Sprintf("FFmpeg streaming failed: %v", err))
			return fmt.Errorf("failed to read from fifo: %w", err)
		}
		return nil
	})

	eg.Go(func() error {
		for {
			select {
			case <-s.ctx.Done():
				s.logger.Debug("Queue processor context cancelled")
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

				s.logger.Info("Processing file",
					zap.String("file", entry.File),
					zap.String("id", entry.ID),
					zap.String("startTimestamp", entry.StartTimestamp),
					zap.String("subtitleFile", entry.SubtitleFile))
				if err := s.writeToFIFO(s.currentCtx, entry.File, entry.Overlay, entry.StartTimestamp, entry.SubtitleFile); err != nil {
					if errors.Is(err, context.Canceled) {
						s.logger.Info("Processing of file was cancelled",
							zap.String("file", entry.File),
							zap.String("id", entry.ID))
					} else {
						s.logger.Error("Failed to write file to fifo",
							zap.String("file", entry.File),
							zap.Error(err))
						s.setError(fmt.Sprintf("FFmpeg processing failed for %s: %v", entry.File, err))
						return fmt.Errorf("ffmpeg failed: %w", err)
					}
					s.mu.Lock()
					s.currentEntry = nil
					s.currentCancel = nil
					s.mu.Unlock()
					continue
				}

				s.logger.Info("Successfully wrote file to fifo", zap.String("file", entry.File))
				s.mu.Lock()
				s.currentEntry = nil
				s.currentCancel = nil
				s.mu.Unlock()
			}
		}
	})

	err := eg.Wait()

	s.logger.Info("StreamManager stopped", zap.Error(err))
	return err
}

func (s *StreamManager) Enqueue(file string, overlay OverlaySettings, startTimestamp string, subtitleFile string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := fmt.Sprintf("%d", time.Now().UnixNano())
	entry := entry{ID: id, File: file, Overlay: overlay, StartTimestamp: startTimestamp, SubtitleFile: subtitleFile}
	s.queue = append(s.queue, entry)

	select {
	case s.queueNotify <- struct{}{}:
	default:
	}

	return id
}

func (s *StreamManager) Dequeue(id string) bool {
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

func (s *StreamManager) Queue() []entry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]entry, len(s.queue))
	copy(result, s.queue)
	return result
}

func (s *StreamManager) Status() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := map[string]any{
		"running":           s.running,
		"activelyStreaming": s.currentEntry != nil,
		"queueLength":       len(s.queue),
	}

	if s.currentEntry != nil {
		status["playing"] = map[string]string{
			"id":   s.currentEntry.ID,
			"file": s.currentEntry.File,
		}
	}

	if s.lastError != "" {
		status["error"] = map[string]any{
			"message": s.lastError,
			"time":    s.lastErrorTime.Unix(),
		}
	}

	return status
}

func (s *StreamManager) setError(errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastError = errMsg
	s.lastErrorTime = time.Now()
}

func (s *StreamManager) Skip() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentCancel != nil {
		s.currentCancel()
		return true
	}
	return false
}

func (s *StreamManager) Stop() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cancel != nil && s.running {
		s.cancel()
		s.running = false // Set immediately so status reflects stopping
		return true
	}
	return false
}

func (s *StreamManager) writeToFIFO(ctx context.Context, source string, overlay OverlaySettings, startTimestamp string, subtitleFile string) error {
	// Validate start timestamp if provided
	if err := s.validateStartTimestamp(ctx, source, startTimestamp); err != nil {
		return fmt.Errorf("timestamp validation failed: %w", err)
	}

	// Validate subtitle file if provided
	if err := s.validateSubtitleFile(subtitleFile); err != nil {
		return fmt.Errorf("subtitle validation failed: %w", err)
	}

	fifo, err := os.OpenFile(s.fifoPath, os.O_WRONLY, os.ModeNamedPipe)
	if err != nil {
		return err
	}
	defer func() {
		if err := fifo.Close(); err != nil {
			s.logger.Warn("Failed to close FIFO", zap.Error(err))
		}
	}()

	args := []string{
		"-hide_banner",
	}

	// Add start timestamp if provided
	if startTimestamp != "" {
		args = append(args, "-ss", startTimestamp)
	}

	args = append(args, "-i", source)

	// Add subtitle input if provided
	if subtitleFile != "" {
		args = append(args, "-i", subtitleFile)
	}

	// Set log level from config or default to error
	logLevel := s.config.LogLevel
	if logLevel == "" {
		logLevel = "error"
	}
	args = append(args, "-loglevel", logLevel)

	// Set default encoder and preset if not specified
	encoder := s.config.Encoder
	if encoder == "" {
		encoder = "libx264"
	}
	preset := s.config.Preset
	if preset == "" {
		preset = "ultrafast"
	}

	// Determine if we need to re-encode in writeToFIFO
	// Only re-encode here for overlays; codec conversion will be handled in readFromFIFO
	needsReencoding := overlay.ShowFilename || subtitleFile != ""

	// Build video filter chain
	var videoFilter string

	// Start with subtitle filter if provided
	if subtitleFile != "" {
		videoFilter = fmt.Sprintf("subtitles='%s'", strings.ReplaceAll(subtitleFile, "'", "\\'"))
	}

	// Add filename overlay if enabled
	if overlay.ShowFilename {
		// Get just the filename without path
		filename := strings.ReplaceAll(source, "\\", "/")
		if idx := strings.LastIndex(filename, "/"); idx != -1 {
			filename = filename[idx+1:]
		}

		// Determine position coordinates
		var x, y string
		switch overlay.Position {
		case "top-left":
			x, y = "10", "10"
		case "top-right":
			x, y = "main_w-text_w-10", "10"
		case "bottom-left":
			x, y = "10", "main_h-text_h-10"
		case "bottom-right":
			x, y = "main_w-text_w-10", "main_h-text_h-10"
		default:
			x, y = "main_w-text_w-10", "main_h-text_h-10"
		}

		filenameFilter := fmt.Sprintf("drawtext=text='%s':fontsize=%d:fontcolor=white:x=%s:y=%s:box=1:boxcolor=black@0.5",
			filename, overlay.FontSize, x, y)

		// Chain filters if subtitle filter exists
		if videoFilter != "" {
			videoFilter = videoFilter + "," + filenameFilter
		} else {
			videoFilter = filenameFilter
		}
	}

	// Apply video filter if any filters are defined
	if videoFilter != "" {
		args = append(args, "-vf", videoFilter)
		args = append(args, "-fps_mode", "vfr")
	}

	// Add encoding settings if re-encoding is needed for overlays
	if needsReencoding {
		args = append(args, "-c:v", encoder, "-preset", preset, "-crf", "18") // Higher quality for intermediate
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c", "copy") // Copy both video and audio if no overlays
	}
	args = append(args, "-f", "mpegts", "pipe:1")

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stdout = fifo

	// Capture stderr for error reporting while also writing to file for preprocessing logs
	var stderrBuf strings.Builder

	// Create temporary log file for ffmpeg preprocessing output
	logFile, err := os.CreateTemp("", "streammanager-ffmpeg-write-*.log")
	if err != nil {
		s.logger.Warn("Failed to create ffmpeg write log file, falling back to stdout", zap.Error(err))
		fallbackWriter := &prefixWriter{
			prefix: "[PREPROCESSING] ",
			writer: os.Stdout,
		}
		cmd.Stderr = io.MultiWriter(&stderrBuf, fallbackWriter)
	} else {
		defer func() {
			logFile.Close()
			// Log the file location for reference
			s.logger.Debug("FFmpeg preprocessing output written to", zap.String("logFile", logFile.Name()))
		}()
		// Create a prefixed writer for preprocessing process identification in log file
		preprocessingWriter := &prefixWriter{
			prefix: "[PREPROCESSING] ",
			writer: logFile,
		}
		cmd.Stderr = io.MultiWriter(&stderrBuf, preprocessingWriter)
	}

	s.logger.Debug("Running ffmpeg write command", zap.Stringer("cmd", cmd))

	err = cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		stderrOutput := strings.TrimSpace(stderrBuf.String())
		if stderrOutput != "" {
			return fmt.Errorf("ffmpeg failed: %w\nFFmpeg stderr: %s", err, stderrOutput)
		}
		return err
	}
	return nil
}

func (s *StreamManager) readFromFIFO(ctx context.Context, fifo string) error {
	// Get the current source file for codec probing
	var sourceFile string
	s.mu.RLock()
	if s.currentEntry != nil {
		sourceFile = s.currentEntry.File
	}
	s.mu.RUnlock()
	dest := s.config.Destination
	if s.config.Username != "" && s.config.Password != "" {
		if strings.HasPrefix(dest, "rtmp://") {
			dest = strings.Replace(dest, "rtmp://", fmt.Sprintf("rtmp://%s:%s@", s.config.Username, s.config.Password), 1)
		}
	}

	// Set log level from config or default to error
	logLevel := s.config.LogLevel
	if logLevel == "" {
		logLevel = "error"
	}

	args := []string{
		"-hide_banner",
		"-loglevel", logLevel,
		"-progress", "pipe:1",
		"-re", "-y",
		"-i", fifo,
		"-fflags", "+igndts",
	}

	// Probe input file once to get all needed information
	// Use source file instead of FIFO to avoid probing issues
	var probeInfo fileProbeInfo
	if sourceFile != "" {
		probeInfo = probeFile(ctx, s.logger, sourceFile)
	}

	// Handle video encoding - consolidate keyframes and bitrate limiting here
	needsVideoReencoding := s.config.KeyframeInterval != "" || s.config.MaxBitrate != "" || probeInfo.needsVideoReencoding

	if needsVideoReencoding {
		// Re-encode video for keyframe interval and/or bitrate limiting
		encoder := s.config.Encoder
		if encoder == "" {
			encoder = "libx264"
		}
		preset := s.config.Preset
		if preset == "" {
			preset = "veryfast" // Faster preset for output encoding
		}

		args = append(args, "-c:v", encoder, "-preset", preset)

		// Force 8-bit output for compatibility when re-encoding due to codec issues
		if probeInfo.needsVideoReencoding {
			args = append(args, "-pix_fmt", "yuv420p") // Force 8-bit
		}

		// Add keyframe settings if specified
		if s.config.KeyframeInterval != "" {
			args = append(args,
				"-g", s.config.KeyframeInterval,
				"-keyint_min", s.config.KeyframeInterval,
			)
		}

		// Add bitrate settings if specified
		if s.config.MaxBitrate != "" {
			args = append(args,
				"-b:v", s.config.MaxBitrate,
				"-maxrate", s.config.MaxBitrate,
				"-bufsize", s.config.MaxBitrate,
			)
		} else {
			args = append(args, "-crf", "23")
		}
	} else {
		// Copy video when no reencoding needed
		args = append(args, "-c:v", "copy")
	}

	// Handle stream mapping if needed (only for complex files with subtitles/many streams)
	if probeInfo.needsExplicitMapping {
		args = append(args,
			"-map", "0:v:0", // Map first video stream
			"-map", "0:a:0", // Map first audio stream only
		)
	}

	// Handle audio encoding based on codec compatibility
	if probeInfo.needsAudioReencoding {
		args = append(args, "-c:a", "aac", "-b:a", "128k", "-ac", "2")
	} else {
		args = append(args, "-c:a", "aac", "-ac", "2")
	}

	args = append(args,
		"-f", "flv",
		"-flvflags", "no_duration_filesize",
		dest,
	)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	// Capture stderr for error reporting while also writing to file and stdout for streaming logs
	var stderrBuf strings.Builder

	// Create temporary log file for ffmpeg streaming output
	logFile, err := os.CreateTemp("", "streammanager-ffmpeg-read-*.log")
	if err != nil {
		s.logger.Warn("Failed to create ffmpeg read log file, falling back to stdout only", zap.Error(err))
		// Fallback to stdout only
		streamingWriter := &prefixWriter{
			prefix: "[STREAMING] ",
			writer: os.Stdout,
		}
		cmd.Stderr = io.MultiWriter(&stderrBuf, streamingWriter)
	} else {
		defer func() {
			logFile.Close()
			// Log the file location for reference
			s.logger.Debug("FFmpeg streaming output written to", zap.String("logFile", logFile.Name()))
		}()

		// Create writers for both file and stdout
		fileWriter := &prefixWriter{
			prefix: "[STREAMING] ",
			writer: logFile,
		}
		stdoutWriter := &prefixWriter{
			prefix: "[STREAMING] ",
			writer: os.Stdout,
		}
		cmd.Stderr = io.MultiWriter(&stderrBuf, fileWriter, stdoutWriter)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	s.logger.Debug("Running ffmpeg read command", zap.Stringer("cmd", cmd))

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Start a goroutine to parse progress data
	go parseProgress(ctx, stdout, s.progressCh)

	err = cmd.Wait()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		stderrOutput := strings.TrimSpace(stderrBuf.String())
		if stderrOutput != "" {
			return fmt.Errorf("ffmpeg failed: %w\nFFmpeg stderr: %s", err, stderrOutput)
		}
		return err
	}
	return nil
}

// GetProgressChan returns a channel that receives progress updates
func (s *StreamManager) GetProgressChan() <-chan progressData {
	return s.progressCh
}

// GetLatestProgress returns the latest progress data (non-blocking)
func (s *StreamManager) GetLatestProgress() (progressData, bool) {
	select {
	case data := <-s.progressCh:
		return data, true
	default:
		return progressData{}, false
	}
}

// ValidateStartTimestamp validates that the start timestamp is not greater than file duration
func (s *StreamManager) ValidateStartTimestamp(ctx context.Context, filePath, startTimestamp string) error {
	return s.validateStartTimestamp(ctx, filePath, startTimestamp)
}

// validateStartTimestamp validates that the start timestamp is not greater than file duration
func (s *StreamManager) validateStartTimestamp(ctx context.Context, filePath, startTimestamp string) error {
	if startTimestamp == "" {
		return nil // No timestamp specified
	}

	// Get file duration using ffprobe
	duration, err := getFileDuration(ctx, filePath)
	if err != nil {
		return fmt.Errorf("failed to get file duration: %w", err)
	}

	// Convert start timestamp to seconds
	startSeconds, err := parseTimestamp(startTimestamp)
	if err != nil {
		return fmt.Errorf("invalid timestamp format: %w", err)
	}

	if startSeconds >= duration {
		return fmt.Errorf("start timestamp (%s) is greater than or equal to file duration (%.2fs)",
			startTimestamp, duration)
	}

	return nil
}

// validateSubtitleFile validates that the subtitle file exists and has a supported format
func (s *StreamManager) validateSubtitleFile(subtitleFile string) error {
	if subtitleFile == "" {
		return nil // No subtitle file specified
	}

	// Check if file exists
	if _, err := os.Stat(subtitleFile); os.IsNotExist(err) {
		return fmt.Errorf("subtitle file does not exist: %s", subtitleFile)
	}

	// Check if the file has a supported subtitle extension
	ext := strings.ToLower(filepath.Ext(subtitleFile))
	supportedExts := []string{".srt", ".vtt", ".ass", ".ssa", ".sub", ".sbv"}

	if slices.Contains(supportedExts, ext) {
		return nil
	}

	return fmt.Errorf("unsupported subtitle format: %s (supported: %s)", ext, strings.Join(supportedExts, ", "))
}
