package streammanager

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestParseProgressData(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected progressData
	}{
		{
			name: "complete progress data",
			input: `frame=1234
fps=30.5
bitrate=1000kbits/s
total_size=5242880
out_time_us=41666666
out_time=00:00:41.66
dup_frames=5
drop_frames=2
speed=1.5x
progress=continue`,
			expected: progressData{
				Frame:      1234,
				Fps:        30.5,
				Bitrate:    "1000kbits/s",
				TotalSize:  5242880,
				OutTimeUs:  41666666,
				OutTime:    "00:00:41.66",
				DupFrames:  5,
				DropFrames: 2,
				Speed:      "1.5x",
				Progress:   "continue",
			},
		},
		{
			name: "partial progress data",
			input: `frame=500
fps=25.0
progress=end`,
			expected: progressData{
				Frame:    500,
				Fps:      25.0,
				Progress: "end",
			},
		},
		{
			name:     "empty input",
			input:    "",
			expected: progressData{},
		},
		{
			name: "malformed lines - no equals",
			input: `frame1234
fps30.5
progress=continue`,
			expected: progressData{
				Progress: "continue",
			},
		},
		{
			name: "malformed lines - multiple equals",
			input: `frame=1234=extra
fps=30.5
progress=continue`,
			expected: progressData{
				Frame:    0, // Should be 0 because "1234=extra" is not a valid integer
				Fps:      30.5,
				Progress: "continue",
			},
		},
		{
			name: "invalid numeric values",
			input: `frame=invalid
fps=not_a_number
total_size=abc
out_time_us=xyz
dup_frames=1.5
drop_frames=true
progress=continue`,
			expected: progressData{
				Progress: "continue",
			},
		},
		{
			name: "empty values",
			input: `frame=
fps=
bitrate=
progress=`,
			expected: progressData{},
		},
		{
			name: "whitespace handling",
			input: `  frame  =  1000  
  fps  =  29.97  
  bitrate  =  2000kbits/s  
  progress  =  continue  `,
			expected: progressData{
				Frame:    1000,
				Fps:      29.97,
				Bitrate:  "2000kbits/s",
				Progress: "continue",
			},
		},
		{
			name: "unknown fields ignored",
			input: `frame=100
unknown_field=value
fps=24.0
another_unknown=123
progress=continue`,
			expected: progressData{
				Frame:    100,
				Fps:      24.0,
				Progress: "continue",
			},
		},
		{
			name: "negative values",
			input: `frame=-1
fps=-30.5
total_size=-1000
progress=continue`,
			expected: progressData{
				Frame:     -1,
				Fps:       -30.5,
				TotalSize: -1000,
				Progress:  "continue",
			},
		},
		{
			name: "zero values",
			input: `frame=0
fps=0.0
total_size=0
out_time_us=0
dup_frames=0
drop_frames=0
progress=continue`,
			expected: progressData{
				Frame:      0,
				Fps:        0.0,
				TotalSize:  0,
				OutTimeUs:  0,
				DupFrames:  0,
				DropFrames: 0,
				Progress:   "continue",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseProgressData(tt.input)

			// Compare all fields except timestamp
			if result.Frame != tt.expected.Frame {
				t.Errorf("Frame: got %d, want %d", result.Frame, tt.expected.Frame)
			}
			if result.Fps != tt.expected.Fps {
				t.Errorf("Fps: got %f, want %f", result.Fps, tt.expected.Fps)
			}
			if result.Bitrate != tt.expected.Bitrate {
				t.Errorf("Bitrate: got %q, want %q", result.Bitrate, tt.expected.Bitrate)
			}
			if result.TotalSize != tt.expected.TotalSize {
				t.Errorf("TotalSize: got %d, want %d", result.TotalSize, tt.expected.TotalSize)
			}
			if result.OutTimeUs != tt.expected.OutTimeUs {
				t.Errorf("OutTimeUs: got %d, want %d", result.OutTimeUs, tt.expected.OutTimeUs)
			}
			if result.OutTime != tt.expected.OutTime {
				t.Errorf("OutTime: got %q, want %q", result.OutTime, tt.expected.OutTime)
			}
			if result.DupFrames != tt.expected.DupFrames {
				t.Errorf("DupFrames: got %d, want %d", result.DupFrames, tt.expected.DupFrames)
			}
			if result.DropFrames != tt.expected.DropFrames {
				t.Errorf("DropFrames: got %d, want %d", result.DropFrames, tt.expected.DropFrames)
			}
			if result.Speed != tt.expected.Speed {
				t.Errorf("Speed: got %q, want %q", result.Speed, tt.expected.Speed)
			}
			if result.Progress != tt.expected.Progress {
				t.Errorf("Progress: got %q, want %q", result.Progress, tt.expected.Progress)
			}

			// Verify timestamp is set and recent
			if result.Timestamp.IsZero() {
				t.Error("Timestamp should be set")
			}
			if time.Since(result.Timestamp) > time.Second {
				t.Error("Timestamp should be recent")
			}
		})
	}
}

func TestParseProgress(t *testing.T) {
	t.Run("single progress block", func(t *testing.T) {
		input := `frame=100
fps=30.0
bitrate=1000kbits/s
progress=continue
frame=200
fps=29.5
progress=end`

		ctx := context.Background()
		out := make(chan progressData, 10)
		reader := strings.NewReader(input)

		go parseProgress(ctx, reader, out)

		// Should receive two progress updates
		select {
		case data := <-out:
			if data.Frame != 100 || data.Fps != 30.0 || data.Bitrate != "1000kbits/s" {
				t.Errorf("First progress block incorrect: frame=%d, fps=%f, bitrate=%s",
					data.Frame, data.Fps, data.Bitrate)
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for first progress update")
		}

		select {
		case data := <-out:
			if data.Frame != 200 || data.Fps != 29.5 {
				t.Errorf("Second progress block incorrect: frame=%d, fps=%f",
					data.Frame, data.Fps)
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for second progress update")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		out := make(chan progressData, 10)

		// Use a pipe to control data flow
		pr, pw := io.Pipe()

		go parseProgress(ctx, pr, out)

		// Send first progress block
		firstBlock := `frame=100
fps=30.0
progress=continue
`
		pw.Write([]byte(firstBlock))

		// Wait for first update and cancel context
		select {
		case <-out:
			cancel()
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for first progress update")
		}

		// Send second progress block after cancellation
		secondBlock := `frame=200
fps=29.5
progress=continue
`
		pw.Write([]byte(secondBlock))
		pw.Close()

		// Should not receive second update due to context cancellation
		select {
		case <-out:
			t.Error("Should not receive progress update after context cancellation")
		case <-time.After(100 * time.Millisecond):
			// Expected behavior
		}
	})

	t.Run("empty progress blocks ignored", func(t *testing.T) {
		input := `progress=continue
frame=100
fps=30.0
progress=continue
progress=end`

		ctx := context.Background()
		out := make(chan progressData, 10)
		reader := strings.NewReader(input)

		go parseProgress(ctx, reader, out)

		// Should only receive one progress update (the one with actual data)
		select {
		case data := <-out:
			if data.Frame != 100 || data.Fps != 30.0 {
				t.Errorf("Progress data incorrect: frame=%d, fps=%f", data.Frame, data.Fps)
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for progress update")
		}

		// Should not receive another update
		select {
		case <-out:
			t.Error("Should not receive second progress update for empty block")
		case <-time.After(100 * time.Millisecond):
			// Expected behavior
		}
	})

	t.Run("channel full - non-blocking", func(t *testing.T) {
		input := `frame=100
progress=continue
frame=200
progress=continue
frame=300
progress=end`

		ctx := context.Background()
		out := make(chan progressData) // Unbuffered channel
		reader := strings.NewReader(input)

		done := make(chan bool)
		go func() {
			parseProgress(ctx, reader, out)
			done <- true
		}()

		// Don't read from channel, so it becomes full immediately
		select {
		case <-done:
			// Should complete without blocking
		case <-time.After(time.Second):
			t.Fatal("parseProgress should not block when channel is full")
		}
	})

	t.Run("malformed input handling", func(t *testing.T) {
		input := `invalid line without progress marker
frame=100
fps=30.0
another invalid line
progress=continue
more invalid content
progress=end`

		ctx := context.Background()
		out := make(chan progressData, 10)
		reader := strings.NewReader(input)

		go parseProgress(ctx, reader, out)

		select {
		case data := <-out:
			if data.Frame != 100 || data.Fps != 30.0 {
				t.Errorf("Progress data incorrect: frame=%d, fps=%f", data.Frame, data.Fps)
			}
		case <-time.After(time.Second):
			t.Fatal("Timeout waiting for progress update")
		}
	})
}

func TestParseProgressTimestamp(t *testing.T) {
	before := time.Now()
	result := parseProgressData("frame=100\nprogress=continue")
	after := time.Now()

	if result.Timestamp.Before(before) || result.Timestamp.After(after) {
		t.Errorf("Timestamp %v should be between %v and %v",
			result.Timestamp, before, after)
	}
}

func TestParseProgressReaderError(t *testing.T) {
	ctx := context.Background()
	out := make(chan progressData, 10)

	// Create a reader that will cause an error
	pr, pw := io.Pipe()
	pw.CloseWithError(io.ErrUnexpectedEOF)

	parseProgress(ctx, pr, out)

	// Should handle the error gracefully and not panic
	select {
	case <-out:
		t.Error("Should not receive any progress data on reader error")
	case <-time.After(100 * time.Millisecond):
		// Expected behavior
	}
}
