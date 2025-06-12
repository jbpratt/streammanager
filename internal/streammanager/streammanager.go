package streammanager

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

// prefixWriter adds a prefix to each line written to the underlying writer
type prefixWriter struct {
	prefix string
	writer io.Writer
}

func (pw *prefixWriter) Write(p []byte) (n int, err error) {
	// Split the input into lines and add prefix to each
	lines := strings.Split(string(p), "\n")
	var prefixedLines []string

	for i, line := range lines {
		if i == len(lines)-1 && line == "" {
			// Don't prefix empty last line (from trailing newline)
			prefixedLines = append(prefixedLines, line)
		} else {
			prefixedLines = append(prefixedLines, pw.prefix+line)
		}
	}

	prefixedOutput := strings.Join(prefixedLines, "\n")
	_, err = pw.writer.Write([]byte(prefixedOutput))
	if err != nil {
		return 0, err
	}

	// Return the number of bytes from the original input
	return len(p), nil
}

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
	if err := s.validateStartTimestamp(source, startTimestamp); err != nil {
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
		probeInfo = s.probeFile(sourceFile)
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
	go s.parseProgress(ctx, stdout)

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

// ffprobeResult represents the output from ffprobe
type ffprobeResult struct {
	Streams []struct {
		Duration string `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// ValidateStartTimestamp validates that the start timestamp is not greater than file duration
func (s *StreamManager) ValidateStartTimestamp(filePath, startTimestamp string) error {
	return s.validateStartTimestamp(filePath, startTimestamp)
}

// validateStartTimestamp validates that the start timestamp is not greater than file duration
func (s *StreamManager) validateStartTimestamp(filePath, startTimestamp string) error {
	if startTimestamp == "" {
		return nil // No timestamp specified
	}

	// Get file duration using ffprobe
	duration, err := s.getFileDuration(filePath)
	if err != nil {
		return fmt.Errorf("failed to get file duration: %w", err)
	}

	// Convert start timestamp to seconds
	startSeconds, err := s.parseTimestamp(startTimestamp)
	if err != nil {
		return fmt.Errorf("invalid timestamp format: %w", err)
	}

	if startSeconds >= duration {
		return fmt.Errorf("start timestamp (%s) is greater than or equal to file duration (%.2fs)",
			startTimestamp, duration)
	}

	return nil
}

// getFileDuration gets the duration of a file using ffprobe
func (s *StreamManager) getFileDuration(filePath string) (float64, error) {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath)

	output, err := cmd.Output()
	if err != nil {
		// Try to get stderr from the exit error
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return 0, fmt.Errorf("ffprobe failed: %w\nFFprobe stderr: %s", err, string(exitErr.Stderr))
		}
		return 0, fmt.Errorf("ffprobe failed: %w", err)
	}

	var result ffprobeResult
	if err := json.Unmarshal(output, &result); err != nil {
		return 0, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	// Try to get duration from format first, then from streams
	var durationStr string
	if result.Format.Duration != "" {
		durationStr = result.Format.Duration
	} else if len(result.Streams) > 0 && result.Streams[0].Duration != "" {
		durationStr = result.Streams[0].Duration
	} else {
		return 0, errors.New("could not determine file duration")
	}

	duration, err := strconv.ParseFloat(durationStr, 64)
	if err != nil {
		return 0, fmt.Errorf("failed to parse duration: %w", err)
	}

	return duration, nil
}

// fileProbeInfo contains all the information we need from ffprobe
type fileProbeInfo struct {
	needsVideoReencoding bool
	needsAudioReencoding bool
	needsExplicitMapping bool
	duration             float64
}

// probeFile runs ffprobe once and extracts all needed information
func (s *StreamManager) probeFile(inputPath string) fileProbeInfo {
	cmd := exec.Command("ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		inputPath,
	)

	output, err := cmd.Output()
	if err != nil {
		s.logger.Warn("Failed to probe file, assuming re-encoding needed", zap.Error(err))
		return fileProbeInfo{
			needsVideoReencoding: true,
			needsAudioReencoding: true,
			needsExplicitMapping: true,
			duration:             0,
		}
	}

	var result struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			PixFmt    string `json:"pix_fmt"`
			Profile   string `json:"profile"`
			Duration  string `json:"duration"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}

	if err := json.Unmarshal(output, &result); err != nil {
		s.logger.Warn("Failed to parse ffprobe output", zap.Error(err))
		return fileProbeInfo{
			needsVideoReencoding: true,
			needsAudioReencoding: true,
			needsExplicitMapping: true,
			duration:             0,
		}
	}

	info := fileProbeInfo{}

	// Analyze streams
	var videoStream, audioStream *struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		PixFmt    string `json:"pix_fmt"`
		Profile   string `json:"profile"`
		Duration  string `json:"duration"`
	}

	hasSubtitles := false
	streamCount := len(result.Streams)

	for i := range result.Streams {
		stream := &result.Streams[i]
		switch stream.CodecType {
		case "video":
			if videoStream == nil {
				videoStream = stream
			}
		case "audio":
			if audioStream == nil {
				audioStream = stream
			}
		case "subtitle":
			hasSubtitles = true
		}
	}

	// Determine video re-encoding needs
	if videoStream != nil {
		switch videoStream.CodecName {
		case "hevc", "h265":
			info.needsVideoReencoding = true
		case "h264", "avc":
			// Check for 10-bit or incompatible profiles
			if strings.Contains(videoStream.PixFmt, "10le") || strings.Contains(videoStream.PixFmt, "10be") {
				info.needsVideoReencoding = true
			}
			if strings.Contains(strings.ToLower(videoStream.Profile), "high 4:4:4") ||
				strings.Contains(strings.ToLower(videoStream.Profile), "high 10") {
				info.needsVideoReencoding = true
			}
		default:
			info.needsVideoReencoding = true
		}
	} else {
		info.needsVideoReencoding = true
	}

	// Determine audio re-encoding needs
	if audioStream != nil {
		switch audioStream.CodecName {
		case "aac", "mp3":
			info.needsAudioReencoding = false
		default:
			info.needsAudioReencoding = true
		}
	} else {
		info.needsAudioReencoding = true
	}

	// Determine explicit mapping needs
	info.needsExplicitMapping = hasSubtitles || streamCount > 5

	// Extract duration
	var durationStr string
	if result.Format.Duration != "" {
		durationStr = result.Format.Duration
	} else if videoStream != nil && videoStream.Duration != "" {
		durationStr = videoStream.Duration
	} else if audioStream != nil && audioStream.Duration != "" {
		durationStr = audioStream.Duration
	}

	if durationStr != "" {
		if duration, err := strconv.ParseFloat(durationStr, 64); err == nil {
			info.duration = duration
		}
	}

	return info
}

// parseTimestamp converts timestamp string to seconds
func (s *StreamManager) parseTimestamp(timestamp string) (float64, error) {
	// Try parsing as seconds first
	if seconds, err := strconv.ParseFloat(timestamp, 64); err == nil {
		return seconds, nil
	}

	// Try parsing as HH:MM:SS format
	timeRegex := regexp.MustCompile(`^(\d{1,2}):(\d{2}):(\d{2})(?:\.(\d+))?$`)
	matches := timeRegex.FindStringSubmatch(timestamp)
	if len(matches) < 4 {
		return 0, errors.New("timestamp must be in HH:MM:SS format or numeric seconds")
	}

	hours, _ := strconv.Atoi(matches[1])
	minutes, _ := strconv.Atoi(matches[2])
	seconds, _ := strconv.Atoi(matches[3])

	var milliseconds float64
	if len(matches) > 4 && matches[4] != "" {
		ms, _ := strconv.ParseFloat("0."+matches[4], 64)
		milliseconds = ms
	}

	totalSeconds := float64(hours*3600+minutes*60+seconds) + milliseconds
	return totalSeconds, nil
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
