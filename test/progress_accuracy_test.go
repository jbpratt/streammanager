package test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jbpratt/streammanager/internal/api"
	"github.com/jbpratt/streammanager/internal/rtmp"
	"github.com/jbpratt/streammanager/internal/webrtc"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestProgressAccuracy(t *testing.T) {
	// Create test logger
	logger := zaptest.NewLogger(t)

	// Start destination RTMP server (where we'll stream TO)
	destRTMPServer, err := rtmp.NewServer(logger, ":1938")
	if err != nil {
		t.Fatalf("Failed to create destination RTMP server: %v", err)
	}

	go func() {
		if err := destRTMPServer.Start(); err != nil {
			logger.Error("Destination RTMP server error", zap.Error(err))
		}
	}()
	defer func() {
		if err := destRTMPServer.Stop(); err != nil {
			logger.Error("Failed to stop destination RTMP server", zap.Error(err))
		}
	}()

	// Give RTMP server time to start
	time.Sleep(100 * time.Millisecond)

	// Create API server with embedded RTMP server
	atomicLevel := zap.NewAtomicLevelAt(zap.InfoLevel)
	apiServer, err := api.New(logger, ":1939", &atomicLevel, "/tmp/streampipe-progress-test.fifo")
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}

	// Create WebRTC server
	webrtcServer, err := webrtc.NewServer(logger)
	if err != nil {
		t.Fatalf("Failed to create WebRTC server: %v", err)
	}

	// Connect WebRTC server to API for status reporting
	apiServer.SetWebRTCServer(webrtcServer)

	// Start HTTP server for API and WebRTC
	mux := http.NewServeMux()
	apiServer.SetupRoutes(mux)
	webrtcServer.SetupRoutes(mux)

	httpServer := &http.Server{
		Addr:    ":8082",
		Handler: mux,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", zap.Error(err))
		}
	}()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Error("Failed to shutdown HTTP server", zap.Error(err))
		}
	}()

	// Give HTTP server time to start
	time.Sleep(100 * time.Millisecond)

	// Start streaming with configuration to our test RTMP server
	config := `{
		"destination": "rtmp://localhost:1938/live/test",
		"rtmpAddr": ":1939",
		"encoder": "libx264",
		"preset": "ultrafast",
		"logLevel": "warning"
	}`

	startResp, err := http.Post("http://localhost:8082/start", "application/json",
		strings.NewReader(config))
	if err != nil {
		t.Fatalf("Failed to start streaming: %v", err)
	}
	startResp.Body.Close()

	if startResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(startResp.Body)
		t.Fatalf("Expected status 200 for start, got %d: %s", startResp.StatusCode, string(body))
	}

	// Give streaming time to start
	time.Sleep(500 * time.Millisecond)

	// Get absolute path to test file (shorter file for faster testing)
	testFile, err := filepath.Abs("out.mp4")
	if err != nil {
		t.Fatalf("Failed to get absolute path to test file: %v", err)
	}

	// Test progress tracking through complete file processing
	t.Run("progress_reaches_100_percent", func(t *testing.T) {
		// Enqueue the file
		reqBody := map[string]any{
			"file": testFile,
		}

		reqBytes, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		resp, err := http.Post("http://localhost:8082/enqueue", "application/json", bytes.NewReader(reqBytes))
		if err != nil {
			t.Fatalf("Failed to enqueue file: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("Expected status 200 for enqueue, got %d: %s", resp.StatusCode, string(body))
		}

		// Wait for streaming to start
		time.Sleep(2 * time.Second)

		var progressHistory []map[string]any
		maxProgress := 0.0
		var finalProgress map[string]any

		// Monitor progress until streaming finishes
		for {
			resp, err := http.Get("http://localhost:8082/progress")
			if err != nil {
				t.Fatalf("Failed to get progress: %v", err)
			}

			var progressResp map[string]any
			json.NewDecoder(resp.Body).Decode(&progressResp)
			resp.Body.Close()

			hasProgress := progressResp["hasProgress"].(bool)
			if hasProgress {
				progress := progressResp["progress"].(map[string]any)
				percentage := progress["percentage"].(float64)

				// Store progress for analysis
				progressHistory = append(progressHistory, progress)

				if percentage > maxProgress {
					maxProgress = percentage
				}

				logger.Info("Progress tracking",
					zap.Float64("percentage", percentage),
					zap.Int64("frame", int64(progress["frame"].(float64))),
					zap.String("out_time", progress["out_time"].(string)),
					zap.Float64("duration", progress["duration"].(float64)))

				finalProgress = progress
			}

			// Check if queue is empty (processing finished)
			queueResp, err := http.Get("http://localhost:8082/queue")
			if err != nil {
				t.Fatalf("Failed to check queue status: %v", err)
			}

			var queueStatus map[string]any
			json.NewDecoder(queueResp.Body).Decode(&queueStatus)
			queueResp.Body.Close()

			status := queueStatus["status"].(map[string]any)
			activelyStreaming := status["activelyStreaming"].(bool)
			queueLength := int(status["queueLength"].(float64))

			if !activelyStreaming && queueLength == 0 {
				logger.Info("Streaming finished",
					zap.Float64("maxProgress", maxProgress),
					zap.Int("progressUpdates", len(progressHistory)))
				break
			}

			time.Sleep(1 * time.Second)
		}

		// Analyze the results
		if len(progressHistory) == 0 {
			t.Fatal("Expected to receive progress updates")
		}

		logger.Info("Progress analysis",
			zap.Float64("maxProgressReached", maxProgress),
			zap.Int("totalProgressUpdates", len(progressHistory)))

		// Log the final progress state
		if finalProgress != nil {
			logger.Info("Final progress state",
				zap.Float64("percentage", finalProgress["percentage"].(float64)),
				zap.String("out_time", finalProgress["out_time"].(string)),
				zap.Float64("duration", finalProgress["duration"].(float64)),
				zap.Int64("frame", int64(finalProgress["frame"].(float64))))
		}

		// The main assertion: progress should reach close to 100%
		// If it stops at ~67-75%, that indicates the issue
		if maxProgress < 95.0 {
			t.Errorf("Progress only reached %.2f%%, expected near 100%%. This indicates preprocessing stops early.", maxProgress)

			// Provide detailed analysis
			t.Logf("Progress analysis:")
			t.Logf("- Maximum progress reached: %.2f%%", maxProgress)
			t.Logf("- Total progress updates: %d", len(progressHistory))
			if finalProgress != nil {
				duration := finalProgress["duration"].(float64)
				outTimeStr := finalProgress["out_time"].(string)
				t.Logf("- Final out_time: %s", outTimeStr)
				t.Logf("- Total duration: %.2f seconds", duration)
				t.Logf("- This suggests FFmpeg preprocessing stopped early")
			}
		} else {
			t.Logf("Progress tracking working correctly, reached %.2f%%", maxProgress)
		}

		// Additional checks for progress consistency
		if len(progressHistory) > 1 {
			firstProgress := progressHistory[0]["percentage"].(float64)
			lastProgress := progressHistory[len(progressHistory)-1]["percentage"].(float64)

			if lastProgress <= firstProgress {
				t.Errorf("Progress did not increase from %.2f%% to %.2f%%", firstProgress, lastProgress)
			}
		}
	})
}
