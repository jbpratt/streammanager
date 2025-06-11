package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/jbpratt/streammanager/internal/api"
	"github.com/jbpratt/streammanager/internal/rtmp"
	"github.com/jbpratt/streammanager/internal/streammanager"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestEndToEnd(t *testing.T) {
	// Create test logger
	logger := zaptest.NewLogger(t)

	// Start destination RTMP server (where we'll stream TO)
	destRTMPServer, err := rtmp.NewServer(logger, ":1936")
	if err != nil {
		t.Fatalf("Failed to create destination RTMP server: %v", err)
	}

	go func() {
		if err := destRTMPServer.Start(); err != nil {
			logger.Error("Destination RTMP server error", zap.Error(err))
		}
	}()
	defer destRTMPServer.Stop()

	// Give RTMP server time to start
	time.Sleep(100 * time.Millisecond)

	// Create API server with embedded RTMP server
	apiServer, err := api.New(logger, ":1937")
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}

	// Start HTTP server for API
	mux := http.NewServeMux()
	apiServer.SetupRoutes(mux)
	
	httpServer := &http.Server{
		Addr:    ":8081",
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
		httpServer.Shutdown(ctx)
	}()

	// Give HTTP server time to start
	time.Sleep(100 * time.Millisecond)

	// Get absolute path to test file
	testFile, err := filepath.Abs("../big_buck_bunny_1080p_h264.mov")
	if err != nil {
		t.Fatalf("Failed to get absolute path to test file: %v", err)
	}

	// Test 1: Start streaming to destination RTMP server
	t.Run("start_streaming", func(t *testing.T) {
		config := streammanager.Config{
			Destination:      "rtmp://localhost:1936/live/test",
			RTMPAddr:         ":1937",
			Encoder:          "libx264",
			Preset:           "ultrafast",
			ProgressEndpoint: "http://localhost:8081/progress",
			LogLevel:         "warning",
		}

		configJSON, err := json.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config: %v", err)
		}

		resp, err := http.Post("http://localhost:8081/start", "application/json", bytes.NewReader(configJSON))
		if err != nil {
			t.Fatalf("Failed to start streaming: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		// Give streaming time to start
		time.Sleep(500 * time.Millisecond)
	})

	// Test 2: Enqueue test file
	t.Run("enqueue_file", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"file": testFile,
			"overlay": map[string]interface{}{
				"showFilename": true,
				"position":     "bottom-right",
				"fontSize":     24,
			},
		}

		reqJSON, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		resp, err := http.Post("http://localhost:8081/enqueue", "application/json", bytes.NewReader(reqJSON))
		if err != nil {
			t.Fatalf("Failed to enqueue file: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		var result map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		if result["id"] == "" {
			t.Fatal("Expected non-empty ID in response")
		}

		if result["file"] != testFile {
			t.Fatalf("Expected file %s, got %s", testFile, result["file"])
		}

		logger.Info("Enqueued file with ID", zap.String("id", result["id"]))
	})

	// Test 3: Check queue status
	t.Run("check_queue", func(t *testing.T) {
		// Wait a bit for file to start processing
		time.Sleep(2 * time.Second)

		resp, err := http.Get("http://localhost:8081/queue")
		if err != nil {
			t.Fatalf("Failed to get queue status: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		var queueStatus map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&queueStatus); err != nil {
			t.Fatalf("Failed to decode queue status: %v", err)
		}

		status := queueStatus["status"].(map[string]interface{})
		
		if !status["running"].(bool) {
			t.Fatal("Expected stream manager to be running")
		}

		logger.Info("Stream manager status", zap.Any("status", status))

		// Check if file is being processed
		if playing, exists := status["playing"]; exists {
			playingMap := playing.(map[string]interface{})
			logger.Info("Currently playing file",
				zap.String("file", playingMap["file"].(string)),
				zap.String("id", playingMap["id"].(string)))
		}
	})

	// Test 4: Check progress updates during processing
	t.Run("check_progress", func(t *testing.T) {
		// Wait for processing to start
		time.Sleep(2 * time.Second)
		
		progressUpdates := 0
		maxChecks := 20
		
		for i := 0; i < maxChecks; i++ {
			resp, err := http.Get("http://localhost:8081/progress")
			if err != nil {
				t.Fatalf("Failed to get progress: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200 for progress, got %d", resp.StatusCode)
			}

			var progressResp map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&progressResp); err != nil {
				t.Fatalf("Failed to decode progress response: %v", err)
			}

			hasProgress := progressResp["hasProgress"].(bool)
			if hasProgress {
				progressUpdates++
				progress := progressResp["progress"].(map[string]interface{})
				
				// Log progress details
				logger.Info("Progress update received",
					zap.Int64("frame", int64(progress["frame"].(float64))),
					zap.Float64("fps", progress["fps"].(float64)),
					zap.String("bitrate", progress["bitrate"].(string)),
					zap.String("out_time", progress["out_time"].(string)),
					zap.String("speed", progress["speed"].(string)))
				
				// Validate progress data structure
				if _, ok := progress["frame"]; !ok {
					t.Fatal("Expected frame field in progress data")
				}
				if _, ok := progress["fps"]; !ok {
					t.Fatal("Expected fps field in progress data")
				}
				if _, ok := progress["timestamp"]; !ok {
					t.Fatal("Expected timestamp field in progress data")
				}
			}
			
			time.Sleep(500 * time.Millisecond)
		}
		
		if progressUpdates == 0 {
			t.Fatal("Expected to receive at least one progress update")
		}
		
		logger.Info("Progress validation completed", zap.Int("total_updates", progressUpdates))
	})

	// Test 5: Let it process for a bit then stop
	t.Run("process_and_stop", func(t *testing.T) {
		// Let it process for 5 more seconds after progress check
		logger.Info("Letting file process for 5 more seconds")
		time.Sleep(5 * time.Second)

		// Stop streaming
		resp, err := http.Post("http://localhost:8081/stop", "application/json", nil)
		if err != nil {
			t.Fatalf("Failed to stop streaming: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		logger.Info("Successfully stopped streaming")

		// Check final status
		time.Sleep(1 * time.Second)
		resp, err = http.Get("http://localhost:8081/queue")
		if err != nil {
			t.Fatalf("Failed to get final queue status: %v", err)
		}
		defer resp.Body.Close()

		var queueStatus map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&queueStatus); err != nil {
			t.Fatalf("Failed to decode final queue status: %v", err)
		}

		status := queueStatus["status"].(map[string]interface{})
		
		if status["running"].(bool) {
			logger.Info("Stream manager is still running (may be shutting down)")
		} else {
			logger.Info("Stream manager has stopped")
		}

		logger.Info("Final status", zap.Any("status", status))
	})
}
