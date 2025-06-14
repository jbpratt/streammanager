package streammanager

import (
	"fmt"
	"strings"
)

type ffmpegArgs struct {
	logLevel         string
	encoder          string
	preset           string
	source           string
	overlay          OverlaySettings
	startTimestamp   string
	subtitleFile     string
	fifoPath         string
	destination      string
	username         string
	password         string
	keyframeInterval string
	maxBitrate       string
	probeInfo        fileProbeInfo
}

// buildFFmpegArgs builds ffmpeg arguments for both preprocessing and streaming
// Mode is determined by which fields are set: Source for preprocessing, FifoPath for streaming
func buildFFmpegArgs(cfg ffmpegArgs) []string {
	if cfg.source != "" {
		return buildPreprocessingArgs(cfg)
	} else if cfg.fifoPath != "" {
		return buildStreamingArgs(cfg)
	}
	return []string{}
}

// buildPreprocessingArgs builds ffmpeg arguments for preprocessing (writeToFIFO)
func buildPreprocessingArgs(cfg ffmpegArgs) []string {
	args := []string{"-hide_banner"}

	// Add start timestamp if provided
	if cfg.startTimestamp != "" {
		args = append(args, "-ss", cfg.startTimestamp)
	}

	args = append(args, "-i", cfg.source)

	// Add subtitle input if provided
	if cfg.subtitleFile != "" {
		args = append(args, "-i", cfg.subtitleFile)
	}

	// Add log level after inputs to match original order
	logLevel := cfg.logLevel
	if logLevel == "" {
		logLevel = "error"
	}
	args = append(args, "-loglevel", logLevel)

	// Always re-encode to ensure compatibility and handle all processing here
	// This includes overlays, subtitles, codec compatibility, and stream standardization

	// Build and apply video filter if needed
	if videoFilter := buildVideoFilter(cfg); videoFilter != "" {
		args = append(args, "-vf", videoFilter, "-fps_mode", "vfr")
	}

	// Always encode video with consistent settings for downstream compatibility
	encoder, preset := getEncoderAndPreset(cfg.encoder, cfg.preset, "ultrafast")
	args = append(args, "-c:v", encoder, "-preset", preset)

	// Add keyframe settings if specified
	if cfg.keyframeInterval != "" {
		args = append(args, "-g", cfg.keyframeInterval, "-keyint_min", cfg.keyframeInterval)
	}

	// Add bitrate settings if specified
	if cfg.maxBitrate != "" {
		args = append(args, "-b:v", cfg.maxBitrate, "-maxrate", cfg.maxBitrate, "-bufsize", cfg.maxBitrate)
	} else {
		args = append(args, "-crf", "18")
	}

	// Force consistent pixel format for compatibility
	args = append(args, "-pix_fmt", "yuv420p")

	// Only add audio encoding if the source file has audio
	if cfg.probeInfo.hasAudio {
		args = append(args, "-c:a", "aac", "-b:a", "128k", "-ac", "2")
	}

	args = append(args, "-f", "mpegts", "pipe:1")
	return args
}

// buildStreamingArgs builds ffmpeg arguments for streaming (readFromFIFO)
func buildStreamingArgs(cfg ffmpegArgs) []string {
	dest := buildDestination(cfg.destination, cfg.username, cfg.password)

	args := buildCommonArgs(cfg.logLevel)
	args = append(args, "-progress", "pipe:1", "-y", "-i", cfg.fifoPath, "-fflags", "+igndts")

	// Use stream copy for both video and audio since all processing is done in writeToFIFO
	args = append(args, "-c", "copy")

	args = append(args,
		"-f", "flv",
		"-flvflags", "no_duration_filesize",
		"-flush_packets", "1",
		"-rtmp_live", "live",
		dest)
	return args
}

// buildCommonArgs builds the common starting arguments for both modes
func buildCommonArgs(logLevel string) []string {
	if logLevel == "" {
		logLevel = "error"
	}
	return []string{"-hide_banner", "-loglevel", logLevel}
}

// getEncoderAndPreset returns the encoder and preset with defaults applied
func getEncoderAndPreset(encoder, preset, defaultPreset string) (string, string) {
	if encoder == "" {
		encoder = "libx264"
	}
	if preset == "" {
		preset = defaultPreset
	}
	return encoder, preset
}

// buildDestination constructs the destination URL with credentials if provided
func buildDestination(destination, username, password string) string {
	if username != "" && password != "" && strings.HasPrefix(destination, "rtmp://") {
		return strings.Replace(destination, "rtmp://", fmt.Sprintf("rtmp://%s:%s@", username, password), 1)
	}
	return destination
}

// buildVideoFilter constructs the video filter chain for preprocessing
func buildVideoFilter(cfg ffmpegArgs) string {
	var filters []string

	// Add subtitle filter if provided
	if cfg.subtitleFile != "" {
		filters = append(filters, fmt.Sprintf("subtitles='%s'", strings.ReplaceAll(cfg.subtitleFile, "'", "\\'")))
	}

	// Add filename overlay if enabled
	if cfg.overlay.ShowFilename {
		filters = append(filters, buildFilenameOverlay(cfg.source, cfg.overlay))
	}

	return strings.Join(filters, ",")
}

// buildFilenameOverlay constructs the drawtext filter for filename overlay
func buildFilenameOverlay(source string, overlay OverlaySettings) string {
	// Extract filename from path
	filename := strings.ReplaceAll(source, "\\", "/")
	if idx := strings.LastIndex(filename, "/"); idx != -1 {
		filename = filename[idx+1:]
	}

	// Get position coordinates
	x, y := getOverlayPosition(overlay.Position)

	return fmt.Sprintf("drawtext=text='%s':fontsize=%d:fontcolor=white:x=%s:y=%s:box=1:boxcolor=black@0.5",
		filename, overlay.FontSize, x, y)
}

// getOverlayPosition returns the x,y coordinates for the overlay position
func getOverlayPosition(position string) (string, string) {
	switch position {
	case "top-left":
		return "10", "10"
	case "top-right":
		return "main_w-text_w-10", "10"
	case "bottom-left":
		return "10", "main_h-text_h-10"
	case "bottom-right":
		return "main_w-text_w-10", "main_h-text_h-10"
	default:
		return "main_w-text_w-10", "main_h-text_h-10"
	}
}
