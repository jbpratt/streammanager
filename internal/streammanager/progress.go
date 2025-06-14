package streammanager

import (
	"bufio"
	"context"
	"io"
	"strconv"
	"strings"
	"time"
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
	Duration   float64   `json:"duration"`   // Total file duration in seconds
	Percentage float64   `json:"percentage"` // Progress percentage (0-100)
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

func parseProgress(ctx context.Context, r io.Reader, out chan progressData, getDuration func() float64) {
	scanner := bufio.NewScanner(r)
	var progressBuffer strings.Builder

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()

		// FFmpeg progress output ends each block with "progress=end" or "progress=continue"
		if strings.HasPrefix(line, "progress=") {
			if progressBuffer.Len() > 0 {
				data := parseProgressData(progressBuffer.String())

				// Get current duration and calculate percentage
				duration := getDuration()
				data.Duration = duration

				if duration > 0 && data.OutTimeUs > 0 {
					currentTimeSeconds := float64(data.OutTimeUs) / 1000000.0 // Convert microseconds to seconds
					data.Percentage = (currentTimeSeconds / duration) * 100.0
					if data.Percentage > 100 {
						data.Percentage = 100
					}
				}

				select {
				case out <- data:
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
