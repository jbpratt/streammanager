package streammanager

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const fifoPath = "/tmp/streampipe.fifo"

type entry struct {
	ID      string          `json:"id"`
	File    string          `json:"file"`
	Overlay OverlaySettings `json:"overlay"`
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
	ProgressEndpoint string `json:"progressEndpoint"`
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
}

func New(logger *zap.Logger) (*StreamManager, error) {
	return &StreamManager{
		mu:          sync.RWMutex{},
		logger:      logger,
		queue:       make([]entry, 0),
		queueNotify: make(chan struct{}, 1),
		progressCh:  make(chan progressData, 100),
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

	_ = os.Remove(fifoPath)
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

	_ = os.Remove(fifoPath)
	if err := syscall.Mkfifo(fifoPath, 0o0644); err != nil {
		return fmt.Errorf("failed to create fifo: %w", err)
	}

	s.logger.Info("StreamManager started")

	eg, ctx := errgroup.WithContext(ctx)
	s.ctx, s.cancel = context.WithCancel(ctx)

	eg.Go(func() error {
		s.logger.Debug("Streaming FIFO reader", zap.String("destination", s.config.Destination))
		if err := s.readFromFIFO(s.ctx, fifoPath); err != nil {
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
					zap.String("id", entry.ID))
				if err := s.writeToFIFO(s.currentCtx, entry.File, entry.Overlay); err != nil {
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

func (s *StreamManager) Enqueue(file string, overlay OverlaySettings) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	id := fmt.Sprintf("%d", time.Now().UnixNano())
	entry := entry{ID: id, File: file, Overlay: overlay}
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

func (s *StreamManager) writeToFIFO(ctx context.Context, source string, overlay OverlaySettings) error {
	fifo, err := os.OpenFile(fifoPath, os.O_WRONLY, os.ModeNamedPipe)
	if err != nil {
		return err
	}
	defer fifo.Close()

	args := []string{
		"-hide_banner",
		"-i", source,
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
	// Only re-encode here for overlays; bitrate limiting will be handled in readFromFIFO
	needsReencoding := overlay.ShowFilename

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

		args = append(args, "-vf", filenameFilter)
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
	// cmd.Stderr = os.Stderr

	s.logger.Debug("Running ffmpeg write command", zap.Stringer("cmd", cmd))

	err = cmd.Run()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

func (s *StreamManager) readFromFIFO(ctx context.Context, fifo string) error {
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

	// Handle video encoding - consolidate keyframes and bitrate limiting here
	needsVideoReencoding := s.config.KeyframeInterval != "" || s.config.MaxBitrate != ""

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

		// Add keyframe settings if specified
		if s.config.KeyframeInterval != "" {
			args = append(args,
				"-g", s.config.KeyframeInterval,
				"-keyint_min", s.config.KeyframeInterval,
				"-no-scenecut", "1", // Disable scene cut for consistent keyframes
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

	args = append(args,
		"-c:a", "aac",
		"-f", "flv",
		"-flvflags", "no_duration_filesize",
		dest,
	)

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	s.logger.Debug("Running ffmpeg read command", zap.Stringer("cmd", cmd))

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	// Start a goroutine to parse progress data
	go s.parseProgress(ctx, stdout)

	err = cmd.Wait()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}

func (s *StreamManager) parseProgress(ctx context.Context, r io.Reader) {
	scanner := bufio.NewScanner(r)
	var progressBuffer strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		// FFmpeg progress output ends each block with "progress=end" or "progress=continue"
		if strings.HasPrefix(line, "progress=") {
			if progressBuffer.Len() > 0 {
				data := parseProgressData(progressBuffer.String())
				select {
				case s.progressCh <- data:
				case <-ctx.Done():
					return
				default:
					// Channel full, skip this update
				}
				progressBuffer.Reset()
			}
		} else if line != "" {
			progressBuffer.WriteString(line)
			progressBuffer.WriteString("\n")
		}
	}
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
