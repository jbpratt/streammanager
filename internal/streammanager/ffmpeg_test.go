package streammanager

import (
	"reflect"
	"slices"
	"testing"
)

func TestBuildFFmpegArgs_Preprocess(t *testing.T) {
	tests := []struct {
		name     string
		cfg      ffmpegArgs
		expected []string
	}{
		{
			name: "basic preprocessing without overlays",
			cfg: ffmpegArgs{
				source: "/path/to/video.mp4",
			},
			expected: []string{
				"-hide_banner",
				"-i", "/path/to/video.mp4",
				"-loglevel", "error",
				"-c", "copy",
				"-f", "mpegts", "pipe:1",
			},
		},
		{
			name: "preprocessing with start timestamp",
			cfg: ffmpegArgs{
				source:         "/path/to/video.mp4",
				startTimestamp: "00:01:30",
			},
			expected: []string{
				"-hide_banner",
				"-ss", "00:01:30",
				"-i", "/path/to/video.mp4",
				"-loglevel", "error",
				"-c", "copy",
				"-f", "mpegts", "pipe:1",
			},
		},
		{
			name: "preprocessing with subtitle file",
			cfg: ffmpegArgs{
				source:       "/path/to/video.mp4",
				subtitleFile: "/path/to/subtitles.srt",
			},
			expected: []string{
				"-hide_banner",
				"-i", "/path/to/video.mp4",
				"-i", "/path/to/subtitles.srt",
				"-loglevel", "error",
				"-vf", "subtitles='/path/to/subtitles.srt'",
				"-fps_mode", "vfr",
				"-c:v", "libx264",
				"-preset", "ultrafast",
				"-crf", "18",
				"-c:a", "copy",
				"-f", "mpegts", "pipe:1",
			},
		},
		{
			name: "preprocessing with filename overlay",
			cfg: ffmpegArgs{
				source: "/path/to/video.mp4",
				overlay: OverlaySettings{
					ShowFilename: true,
					Position:     "bottom-right",
					FontSize:     16,
				},
			},
			expected: []string{
				"-hide_banner",
				"-i", "/path/to/video.mp4",
				"-loglevel", "error",
				"-vf", "drawtext=text='video.mp4':fontsize=16:fontcolor=white:x=main_w-text_w-10:y=main_h-text_h-10:box=1:boxcolor=black@0.5",
				"-fps_mode", "vfr",
				"-c:v", "libx264",
				"-preset", "ultrafast",
				"-crf", "18",
				"-c:a", "copy",
				"-f", "mpegts", "pipe:1",
			},
		},
		{
			name: "preprocessing with subtitle and overlay",
			cfg: ffmpegArgs{
				source:       "/path/to/video.mp4",
				subtitleFile: "/path/to/subtitles.srt",
				overlay: OverlaySettings{
					ShowFilename: true,
					Position:     "top-left",
					FontSize:     20,
				},
			},
			expected: []string{
				"-hide_banner",
				"-i", "/path/to/video.mp4",
				"-i", "/path/to/subtitles.srt",
				"-loglevel", "error",
				"-vf", "subtitles='/path/to/subtitles.srt',drawtext=text='video.mp4':fontsize=20:fontcolor=white:x=10:y=10:box=1:boxcolor=black@0.5",
				"-fps_mode", "vfr",
				"-c:v", "libx264",
				"-preset", "ultrafast",
				"-crf", "18",
				"-c:a", "copy",
				"-f", "mpegts", "pipe:1",
			},
		},
		{
			name: "preprocessing with custom encoder and preset",
			cfg: ffmpegArgs{
				source:  "/path/to/video.mp4",
				encoder: "libx265",
				preset:  "medium",
				overlay: OverlaySettings{
					ShowFilename: true,
					Position:     "bottom-left",
					FontSize:     12,
				},
			},
			expected: []string{
				"-hide_banner",
				"-i", "/path/to/video.mp4",
				"-loglevel", "error",
				"-vf", "drawtext=text='video.mp4':fontsize=12:fontcolor=white:x=10:y=main_h-text_h-10:box=1:boxcolor=black@0.5",
				"-fps_mode", "vfr",
				"-c:v", "libx265",
				"-preset", "medium",
				"-crf", "18",
				"-c:a", "copy",
				"-f", "mpegts", "pipe:1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildFFmpegArgs(tt.cfg)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("BuildFFmpegArgs() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBuildFFmpegArgs_Stream(t *testing.T) {
	tests := []struct {
		name     string
		cfg      ffmpegArgs
		expected []string
	}{
		{
			name: "basic streaming without reencoding",
			cfg: ffmpegArgs{
				fifoPath:    "/tmp/fifo",
				destination: "rtmp://example.com/live/stream",
				probeInfo: fileProbeInfo{
					needsVideoReencoding: false,
					needsAudioReencoding: false,
					needsExplicitMapping: false,
				},
			},
			expected: []string{
				"-hide_banner",
				"-loglevel", "error",
				"-progress", "pipe:1",
				"-re", "-y",
				"-i", "/tmp/fifo",
				"-fflags", "+igndts",
				"-c:v", "copy",
				"-c:a", "aac",
				"-ac", "2",
				"-f", "flv",
				"-flvflags", "no_duration_filesize",
				"rtmp://example.com/live/stream",
			},
		},
		{
			name: "streaming with video reencoding due to codec",
			cfg: ffmpegArgs{
				fifoPath:    "/tmp/fifo",
				destination: "rtmp://example.com/live/stream",
				probeInfo: fileProbeInfo{
					needsVideoReencoding: true,
					needsAudioReencoding: false,
					needsExplicitMapping: false,
				},
			},
			expected: []string{
				"-hide_banner",
				"-loglevel", "error",
				"-progress", "pipe:1",
				"-re", "-y",
				"-i", "/tmp/fifo",
				"-fflags", "+igndts",
				"-c:v", "libx264",
				"-preset", "veryfast",
				"-pix_fmt", "yuv420p",
				"-crf", "23",
				"-c:a", "aac",
				"-ac", "2",
				"-f", "flv",
				"-flvflags", "no_duration_filesize",
				"rtmp://example.com/live/stream",
			},
		},
		{
			name: "streaming with keyframe interval",
			cfg: ffmpegArgs{
				fifoPath:         "/tmp/fifo",
				destination:      "rtmp://example.com/live/stream",
				keyframeInterval: "60",
				probeInfo: fileProbeInfo{
					needsVideoReencoding: false,
					needsAudioReencoding: false,
					needsExplicitMapping: false,
				},
			},
			expected: []string{
				"-hide_banner",
				"-loglevel", "error",
				"-progress", "pipe:1",
				"-re", "-y",
				"-i", "/tmp/fifo",
				"-fflags", "+igndts",
				"-c:v", "libx264",
				"-preset", "veryfast",
				"-g", "60",
				"-keyint_min", "60",
				"-crf", "23",
				"-c:a", "aac",
				"-ac", "2",
				"-f", "flv",
				"-flvflags", "no_duration_filesize",
				"rtmp://example.com/live/stream",
			},
		},
		{
			name: "streaming with bitrate limiting",
			cfg: ffmpegArgs{
				fifoPath:    "/tmp/fifo",
				destination: "rtmp://example.com/live/stream",
				maxBitrate:  "2000k",
				probeInfo: fileProbeInfo{
					needsVideoReencoding: false,
					needsAudioReencoding: false,
					needsExplicitMapping: false,
				},
			},
			expected: []string{
				"-hide_banner",
				"-loglevel", "error",
				"-progress", "pipe:1",
				"-re", "-y",
				"-i", "/tmp/fifo",
				"-fflags", "+igndts",
				"-c:v", "libx264",
				"-preset", "veryfast",
				"-b:v", "2000k",
				"-maxrate", "2000k",
				"-bufsize", "2000k",
				"-c:a", "aac",
				"-ac", "2",
				"-f", "flv",
				"-flvflags", "no_duration_filesize",
				"rtmp://example.com/live/stream",
			},
		},
		{
			name: "streaming with auth credentials",
			cfg: ffmpegArgs{
				fifoPath:    "/tmp/fifo",
				destination: "rtmp://example.com/live/stream",
				username:    "user123",
				password:    "pass456",
				probeInfo: fileProbeInfo{
					needsVideoReencoding: false,
					needsAudioReencoding: false,
					needsExplicitMapping: false,
				},
			},
			expected: []string{
				"-hide_banner",
				"-loglevel", "error",
				"-progress", "pipe:1",
				"-re", "-y",
				"-i", "/tmp/fifo",
				"-fflags", "+igndts",
				"-c:v", "copy",
				"-c:a", "aac",
				"-ac", "2",
				"-f", "flv",
				"-flvflags", "no_duration_filesize",
				"rtmp://user123:pass456@example.com/live/stream",
			},
		},
		{
			name: "streaming with complex file requiring mapping",
			cfg: ffmpegArgs{
				fifoPath:    "/tmp/fifo",
				destination: "rtmp://example.com/live/stream",
				probeInfo: fileProbeInfo{
					needsVideoReencoding: false,
					needsAudioReencoding: true,
					needsExplicitMapping: true,
				},
			},
			expected: []string{
				"-hide_banner",
				"-loglevel", "error",
				"-progress", "pipe:1",
				"-re", "-y",
				"-i", "/tmp/fifo",
				"-fflags", "+igndts",
				"-c:v", "copy",
				"-map", "0:v:0",
				"-map", "0:a:0",
				"-c:a", "aac",
				"-b:a", "128k",
				"-ac", "2",
				"-f", "flv",
				"-flvflags", "no_duration_filesize",
				"rtmp://example.com/live/stream",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildFFmpegArgs(tt.cfg)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("BuildFFmpegArgs() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestBuildFFmpegArgs_RealWorldExamples(t *testing.T) {
	// Based on probe info from meme.mkv (HEVC 10-bit, E-AC3, many subtitle streams)
	memeProbeInfo := fileProbeInfo{
		needsVideoReencoding: true, // HEVC + 10-bit
		needsAudioReencoding: true, // E-AC3
		needsExplicitMapping: true, // 30 streams
	}

	// Based on probe info from big_buck_bunny_1080p_h264.mov (H264 8-bit, AAC 5.1)
	bunnyProbeInfo := fileProbeInfo{
		needsVideoReencoding: false, // H264 8-bit
		needsAudioReencoding: false, // AAC
		needsExplicitMapping: false, // 3 streams
	}

	tests := []struct {
		name string
		cfg  ffmpegArgs
	}{
		{
			name: "meme.mkv preprocessing with overlay",
			cfg: ffmpegArgs{
				source: "meme.mkv",
				overlay: OverlaySettings{
					ShowFilename: true,
					Position:     "bottom-right",
					FontSize:     16,
				},
			},
		},
		{
			name: "meme.mkv streaming",
			cfg: ffmpegArgs{
				fifoPath:         "/tmp/fifo",
				destination:      "rtmp://live.twitch.tv/live/your_stream_key",
				keyframeInterval: "60",
				maxBitrate:       "6000k",
				probeInfo:        memeProbeInfo,
			},
		},
		{
			name: "big_buck_bunny preprocessing",
			cfg: ffmpegArgs{
				source:         "big_buck_bunny_1080p_h264.mov",
				startTimestamp: "00:05:00",
			},
		},
		{
			name: "big_buck_bunny streaming",
			cfg: ffmpegArgs{
				fifoPath:    "/tmp/fifo",
				destination: "rtmp://live.youtube.com/live2/your_stream_key",
				maxBitrate:  "4500k",
				probeInfo:   bunnyProbeInfo,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildFFmpegArgs(tt.cfg)
			// Verify the result is not empty and contains expected patterns
			if len(result) == 0 {
				t.Errorf("BuildFFmpegArgs() returned empty result")
			}

			// Check for required args based on mode (inferred from config)
			if tt.cfg.source != "" {
				// Preprocessing mode
				if !slices.Contains(result, "-i") || !slices.Contains(result, "-f") || !slices.Contains(result, "mpegts") {
					t.Errorf("preprocessing args missing required elements: %v", result)
				}
			} else if tt.cfg.fifoPath != "" {
				// Streaming mode
				if !slices.Contains(result, "-progress") || !slices.Contains(result, "-re") || !slices.Contains(result, "flv") {
					t.Errorf("streaming args missing required elements: %v", result)
				}
			}
		})
	}
}
