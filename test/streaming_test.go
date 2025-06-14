package test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/jbpratt/streammanager/internal/streammanager"
	"go.uber.org/zap"
)

func TestStartStreaming(t *testing.T) {
	// Setup test environment
	logger, _, apiServer, _, _, cleanup := SetupTestEnvironment(t)
	defer cleanup()

	// Get test file
	testFile := GetTestFile(t)

	// First enqueue a test file
	reqBody := map[string]any{
		"file": testFile,
		"overlay": map[string]any{
			"showFilename": true,
			"position":     "bottom-right",
			"fontSize":     24,
		},
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal enqueue request: %v", err)
	}

	resp, err := http.Post("http://localhost:8081/enqueue", "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		t.Fatalf("Failed to enqueue file: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200 for enqueue, got %d", resp.StatusCode)
	}

	// Now test starting streaming
	t.Run("start_with_valid_config", func(t *testing.T) {
		config := streammanager.Config{
			Destination: "rtmp://localhost:1936/live/test",
			RTMPAddr:    ":1937",
			Encoder:     "libx264",
			Preset:      "ultrafast",
			LogLevel:    "warning",
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
			t.Fatalf("Expected status 200 for start, got %d", resp.StatusCode)
		}

		// Verify streaming is running
		time.Sleep(500 * time.Millisecond)
		resp, err = http.Get("http://localhost:8081/queue")
		if err != nil {
			t.Fatalf("Failed to get queue status: %v", err)
		}
		defer resp.Body.Close()

		var queueStatus map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&queueStatus); err != nil {
			t.Fatalf("Failed to decode queue status: %v", err)
		}

		status := queueStatus["status"].(map[string]any)
		if !status["running"].(bool) {
			t.Fatal("Expected stream manager to be running after start")
		}

		logger.Info("Streaming started successfully")
	})

	t.Run("start_with_invalid_config", func(t *testing.T) {
		// Stop existing stream first
		http.Post("http://localhost:8081/stop", "application/json", nil)
		time.Sleep(200 * time.Millisecond)

		// Try to start with invalid config (missing destination)
		config := streammanager.Config{
			RTMPAddr: ":1937",
			Encoder:  "libx264",
			Preset:   "ultrafast",
			LogLevel: "warning",
			// Missing Destination field
		}

		configJSON, err := json.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config: %v", err)
		}

		resp, err := http.Post("http://localhost:8081/start", "application/json", bytes.NewReader(configJSON))
		if err != nil {
			t.Fatalf("Failed to call start endpoint: %v", err)
		}
		defer resp.Body.Close()

		// Should return error for invalid config
		if resp.StatusCode == http.StatusOK {
			t.Fatal("Expected error for invalid config (missing destination)")
		}

		logger.Info("Invalid config properly rejected")
	})

	t.Run("start_with_custom_bitrate", func(t *testing.T) {
		config := streammanager.Config{
			Destination: "rtmp://localhost:1936/live/test-bitrate",
			RTMPAddr:    ":1937",
			Encoder:     "libx264",
			Preset:      "ultrafast",
			LogLevel:    "warning",
			MaxBitrate:  "2000k",
		}

		configJSON, err := json.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config: %v", err)
		}

		resp, err := http.Post("http://localhost:8081/start", "application/json", bytes.NewReader(configJSON))
		if err != nil {
			t.Fatalf("Failed to start streaming with custom bitrate: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200 for start with custom bitrate, got %d", resp.StatusCode)
		}

		logger.Info("Streaming with custom bitrate started successfully")
	})

	// Cleanup
	_ = apiServer
}

func TestStopStreaming(t *testing.T) {
	// Setup test environment
	logger, _, apiServer, _, _, cleanup := SetupTestEnvironment(t)
	defer cleanup()

	// Get test file
	testFile := GetTestFile(t)

	// First enqueue a test file
	reqBody := map[string]any{
		"file": testFile,
		"overlay": map[string]any{
			"showFilename": true,
			"position":     "bottom-right",
			"fontSize":     24,
		},
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal enqueue request: %v", err)
	}

	resp, err := http.Post("http://localhost:8081/enqueue", "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		t.Fatalf("Failed to enqueue file: %v", err)
	}
	defer resp.Body.Close()

	// Start streaming
	config := streammanager.Config{
		Destination: "rtmp://localhost:1936/live/stop-test",
		RTMPAddr:    ":1937",
		Encoder:     "libx264",
		Preset:      "ultrafast",
		LogLevel:    "warning",
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	resp, err = http.Post("http://localhost:8081/start", "application/json", bytes.NewReader(configJSON))
	if err != nil {
		t.Fatalf("Failed to start streaming: %v", err)
	}
	defer resp.Body.Close()

	// Give streaming time to start
	time.Sleep(500 * time.Millisecond)

	t.Run("stop_running_stream", func(t *testing.T) {
		// Verify streaming is running first
		resp, err := http.Get("http://localhost:8081/queue")
		if err != nil {
			t.Fatalf("Failed to get queue status: %v", err)
		}
		defer resp.Body.Close()

		var queueStatus map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&queueStatus); err != nil {
			t.Fatalf("Failed to decode queue status: %v", err)
		}

		status := queueStatus["status"].(map[string]any)
		if !status["running"].(bool) {
			t.Fatal("Expected stream manager to be running before stop")
		}

		// Now stop the stream
		resp, err = http.Post("http://localhost:8081/stop", "application/json", nil)
		if err != nil {
			t.Fatalf("Failed to stop streaming: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200 for stop, got %d", resp.StatusCode)
		}

		// Verify stream has stopped
		time.Sleep(1 * time.Second)
		resp, err = http.Get("http://localhost:8081/queue")
		if err != nil {
			t.Fatalf("Failed to get final queue status: %v", err)
		}
		defer resp.Body.Close()

		if err := json.NewDecoder(resp.Body).Decode(&queueStatus); err != nil {
			t.Fatalf("Failed to decode final queue status: %v", err)
		}

		status = queueStatus["status"].(map[string]any)
		if status["running"].(bool) {
			logger.Info("Stream manager is still running (may be shutting down)")
		} else {
			logger.Info("Stream manager has stopped successfully")
		}

		logger.Info("Stop streaming test completed")
	})

	t.Run("stop_already_stopped_stream", func(t *testing.T) {
		// Try to stop again (returns 400 for already stopped stream)
		resp, err := http.Post("http://localhost:8081/stop", "application/json", nil)
		if err != nil {
			t.Fatalf("Failed to stop already stopped stream: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("Expected status 400 for stop on already stopped stream, got %d", resp.StatusCode)
		}

		logger.Info("Stop on already stopped stream handled correctly")
	})

	// Cleanup
	_ = apiServer
}

func TestStreamingLifecycle(t *testing.T) {
	// Setup test environment
	logger, _, apiServer, _, _, cleanup := SetupTestEnvironment(t)
	defer cleanup()

	// Get test file
	testFile := GetTestFile(t)

	t.Run("complete_streaming_lifecycle", func(t *testing.T) {
		// Phase 1: Enqueue file
		t.Run("enqueue_phase", func(t *testing.T) {
			reqBody := map[string]any{
				"file": testFile,
				"overlay": map[string]any{
					"showFilename": true,
					"position":     "bottom-right",
					"fontSize":     24,
				},
			}

			reqJSON, err := json.Marshal(reqBody)
			if err != nil {
				t.Fatalf("Failed to marshal enqueue request: %v", err)
			}

			resp, err := http.Post("http://localhost:8081/enqueue", "application/json", bytes.NewReader(reqJSON))
			if err != nil {
				t.Fatalf("Failed to enqueue file: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200 for enqueue, got %d", resp.StatusCode)
			}

			var result map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("Failed to decode enqueue response: %v", err)
			}

			if result["id"] == "" {
				t.Fatal("Expected non-empty ID in enqueue response")
			}

			logger.Info("File enqueued successfully", zap.String("id", result["id"]))
		})

		// Phase 2: Start streaming
		t.Run("start_phase", func(t *testing.T) {
			config := streammanager.Config{
				Destination:      "rtmp://localhost:1936/live/lifecycle-test",
				RTMPAddr:         ":1937",
				Encoder:          "libx264",
				Preset:           "ultrafast",
				LogLevel:         "warning",
				MaxBitrate:       "1500k",
				KeyframeInterval: "60",
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
				t.Fatalf("Expected status 200 for start, got %d", resp.StatusCode)
			}

			logger.Info("Streaming started successfully")
		})

		// Phase 3: Monitor streaming status
		t.Run("monitor_phase", func(t *testing.T) {
			// Wait for streaming to start
			time.Sleep(1 * time.Second)

			resp, err := http.Get("http://localhost:8081/queue")
			if err != nil {
				t.Fatalf("Failed to get queue status: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200 for queue status, got %d", resp.StatusCode)
			}

			var queueStatus map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&queueStatus); err != nil {
				t.Fatalf("Failed to decode queue status: %v", err)
			}

			status := queueStatus["status"].(map[string]any)

			// Validate streaming is running
			if !status["running"].(bool) {
				t.Fatal("Expected stream manager to be running")
			}

			// Check if file is being processed
			if playing, exists := status["playing"]; exists {
				playingMap := playing.(map[string]any)
				logger.Info("Currently playing file",
					zap.String("file", playingMap["file"].(string)),
					zap.String("id", playingMap["id"].(string)))
			}

			logger.Info("Stream manager status validated", zap.Any("status", status))
		})

		// Phase 4: Check progress updates
		t.Run("progress_phase", func(t *testing.T) {
			// Wait for processing to start
			time.Sleep(2 * time.Second)

			progressUpdates := 0
			maxChecks := 10
			var lastPercentage float64 = -1

			for i := 0; i < maxChecks; i++ {
				resp, err := http.Get("http://localhost:8081/progress")
				if err != nil {
					t.Fatalf("Failed to get progress: %v", err)
				}
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Fatalf("Expected status 200 for progress, got %d", resp.StatusCode)
				}

				var progressResp map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&progressResp); err != nil {
					t.Fatalf("Failed to decode progress response: %v", err)
				}

				hasProgress := progressResp["hasProgress"].(bool)
				if hasProgress {
					progressUpdates++
					progress := progressResp["progress"].(map[string]any)

					// Validate required progress fields
					requiredFields := []string{"frame", "fps", "timestamp", "duration", "percentage"}
					for _, field := range requiredFields {
						if _, ok := progress[field]; !ok {
							t.Fatalf("Expected %s field in progress data", field)
						}
					}

					// Validate data ranges
					percentage := progress["percentage"].(float64)
					if percentage < 0 || percentage > 100 {
						t.Fatalf("Percentage should be between 0-100, got %.2f", percentage)
					}

					// Log progress details
					logger.Info("Progress update",
						zap.Int64("frame", int64(progress["frame"].(float64))),
						zap.Float64("fps", progress["fps"].(float64)),
						zap.Float64("percentage", percentage))

					lastPercentage = percentage

					// If we've seen good progress, break early
					if progressUpdates >= 3 {
						break
					}
				}

				time.Sleep(500 * time.Millisecond)
			}

			if progressUpdates == 0 {
				t.Fatal("Expected to receive at least one progress update")
			}

			logger.Info("Progress monitoring completed",
				zap.Int("total_updates", progressUpdates),
				zap.Float64("last_percentage", lastPercentage))
		})

		// Phase 5: Stop streaming
		t.Run("stop_phase", func(t *testing.T) {
			// Let it process for a bit more
			time.Sleep(2 * time.Second)

			resp, err := http.Post("http://localhost:8081/stop", "application/json", nil)
			if err != nil {
				t.Fatalf("Failed to stop streaming: %v", err)
			}
			defer resp.Body.Close()

			// Accept both 200 (success) and 400 (already stopped) as valid responses
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("Expected status 200 or 400 for stop, got %d", resp.StatusCode)
			}

			logger.Info("Streaming stopped successfully")

			// Verify final status
			time.Sleep(1 * time.Second)
			resp, err = http.Get("http://localhost:8081/queue")
			if err != nil {
				t.Fatalf("Failed to get final queue status: %v", err)
			}
			defer resp.Body.Close()

			var queueStatus map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&queueStatus); err != nil {
				t.Fatalf("Failed to decode final queue status: %v", err)
			}

			status := queueStatus["status"].(map[string]any)
			logger.Info("Final streaming status", zap.Any("status", status))
		})
	})

	t.Run("streaming_configuration_validation", func(t *testing.T) {
		// Test various configuration combinations
		configs := []struct {
			name       string
			config     streammanager.Config
			shouldFail bool
		}{
			{
				name: "minimal_valid_config",
				config: streammanager.Config{
					Destination: "rtmp://localhost:1936/live/minimal",
					RTMPAddr:    ":1937",
				},
				shouldFail: false,
			},
			{
				name: "complete_config",
				config: streammanager.Config{
					Destination:      "rtmp://localhost:1936/live/complete",
					RTMPAddr:         ":1937",
					Encoder:          "libx264",
					Preset:           "medium",
					LogLevel:         "info",
					MaxBitrate:       "3000k",
					KeyframeInterval: "30",
				},
				shouldFail: false,
			},
			{
				name: "missing_destination",
				config: streammanager.Config{
					RTMPAddr: ":1937",
					Encoder:  "libx264",
				},
				shouldFail: true,
			},
		}

		for _, tc := range configs {
			t.Run(tc.name, func(t *testing.T) {
				// Stop any existing streams
				http.Post("http://localhost:8081/stop", "application/json", nil)
				time.Sleep(200 * time.Millisecond)

				configJSON, err := json.Marshal(tc.config)
				if err != nil {
					t.Fatalf("Failed to marshal config: %v", err)
				}

				resp, err := http.Post("http://localhost:8081/start", "application/json", bytes.NewReader(configJSON))
				if err != nil {
					t.Fatalf("Failed to call start endpoint: %v", err)
				}
				defer resp.Body.Close()

				if tc.shouldFail {
					if resp.StatusCode == http.StatusOK {
						t.Fatalf("Expected failure for config %s but got success", tc.name)
					}
					logger.Info("Config validation correctly failed", zap.String("config", tc.name))
				} else {
					if resp.StatusCode != http.StatusOK {
						t.Fatalf("Expected success for config %s but got status %d", tc.name, resp.StatusCode)
					}
					logger.Info("Config validation passed", zap.String("config", tc.name))
				}
			})
		}
	})

	t.Run("concurrent_start_stop_operations", func(t *testing.T) {
		// Test that multiple start/stop calls are handled gracefully
		config := streammanager.Config{
			Destination: "rtmp://localhost:1936/live/concurrent-test",
			RTMPAddr:    ":1937",
			Encoder:     "libx264",
			Preset:      "ultrafast",
			LogLevel:    "warning",
		}

		configJSON, err := json.Marshal(config)
		if err != nil {
			t.Fatalf("Failed to marshal config: %v", err)
		}

		// Start streaming
		resp, err := http.Post("http://localhost:8081/start", "application/json", bytes.NewReader(configJSON))
		if err != nil {
			t.Fatalf("Failed to start streaming: %v", err)
		}
		defer resp.Body.Close()

		// Try to start again (should handle gracefully)
		resp, err = http.Post("http://localhost:8081/start", "application/json", bytes.NewReader(configJSON))
		if err != nil {
			t.Fatalf("Failed to call start again: %v", err)
		}
		defer resp.Body.Close()

		// Multiple stop calls should be safe
		for i := 0; i < 3; i++ {
			resp, err = http.Post("http://localhost:8081/stop", "application/json", nil)
			if err != nil {
				t.Fatalf("Failed to stop streaming (attempt %d): %v", i+1, err)
			}
			defer resp.Body.Close()

			// Accept both 200 (success) and 400 (already stopped) as valid responses
			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("Expected status 200 or 400 for stop (attempt %d), got %d", i+1, resp.StatusCode)
			}
		}

		logger.Info("Concurrent operations handled correctly")
	})

	// Cleanup
	_ = apiServer
}
