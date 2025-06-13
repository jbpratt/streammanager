package streammanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// ffprobeResult represents the output from ffprobe
type ffprobeResult struct {
	Streams []struct {
		Duration string `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

// fileProbeInfo contains all the information we need from ffprobe
type fileProbeInfo struct {
	needsVideoReencoding bool
	needsAudioReencoding bool
	needsExplicitMapping bool
	duration             float64
}

// getFileDuration gets the duration of a file using ffprobe
func getFileDuration(ctx context.Context, filePath string) (float64, error) {
	cmd := exec.CommandContext(ctx, "ffprobe",
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

// probeFile runs ffprobe once and extracts all needed information
func probeFile(ctx context.Context, logger *zap.Logger, inputPath string) fileProbeInfo {
	cmd := exec.CommandContext(ctx, "ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		inputPath,
	)

	output, err := cmd.Output()
	if err != nil {
		logger.Warn("Failed to probe file, assuming re-encoding needed", zap.Error(err))
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
		logger.Warn("Failed to parse ffprobe output", zap.Error(err))
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
func parseTimestamp(timestamp string) (float64, error) {
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
