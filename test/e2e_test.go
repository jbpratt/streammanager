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
	"github.com/jbpratt/streammanager/internal/streammanager"
	"github.com/jbpratt/streammanager/internal/webrtc"
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
	defer func() {
		if err := destRTMPServer.Stop(); err != nil {
			logger.Error("Failed to stop destination RTMP server", zap.Error(err))
		}
	}()

	// Give RTMP server time to start
	time.Sleep(100 * time.Millisecond)

	// Create API server with embedded RTMP server
	apiServer, err := api.New(logger, ":1937")
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
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Error("Failed to shutdown HTTP server", zap.Error(err))
		}
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

	// Test 6: WebRTC endpoints validation
	t.Run("webrtc_endpoints", func(t *testing.T) {
		// Test WebRTC status endpoint
		t.Run("webrtc_status", func(t *testing.T) {
			resp, err := http.Get("http://localhost:8081/webrtc/status")
			if err != nil {
				t.Fatalf("Failed to get WebRTC status: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200 for WebRTC status, got %d", resp.StatusCode)
			}

			var status map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
				t.Fatalf("Failed to decode WebRTC status: %v", err)
			}

			// Validate required fields
			requiredFields := []string{"broadcasting", "subscribers_count", "has_video", "has_audio"}
			for _, field := range requiredFields {
				if _, exists := status[field]; !exists {
					t.Fatalf("Expected field %s in WebRTC status", field)
				}
			}

			// Initially, there should be no broadcast
			if status["broadcasting"].(bool) {
				t.Fatal("Expected broadcasting to be false initially")
			}

			if status["subscribers_count"].(float64) != 0 {
				t.Fatal("Expected subscribers_count to be 0 initially")
			}

			logger.Info("WebRTC status validated", zap.Any("status", status))
		})

		// Test WHIP endpoint with invalid SDP (should return 400)
		t.Run("whip_invalid_sdp", func(t *testing.T) {
			invalidSDP := "invalid sdp content"
			resp, err := http.Post("http://localhost:8081/whip", "application/sdp", strings.NewReader(invalidSDP))
			if err != nil {
				t.Fatalf("Failed to call WHIP endpoint: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("Expected status 400 for invalid SDP, got %d", resp.StatusCode)
			}

			logger.Info("WHIP invalid SDP test passed")
		})

		// Test WHEP endpoint with no active broadcast (should return 404)
		t.Run("whep_no_broadcast", func(t *testing.T) {
			validOfferSDP := `v=0
o=- 123456789 123456789 IN IP4 127.0.0.1
s=-
t=0 0
m=video 9 UDP/TLS/RTP/SAVPF 96
c=IN IP4 0.0.0.0
a=rtcp:9 IN IP4 0.0.0.0
a=ice-ufrag:test
a=ice-pwd:testpassword
a=fingerprint:sha-256 00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00:00
a=setup:actpass
a=mid:0
a=sendrecv
a=rtcp-mux
a=rtpmap:96 H264/90000`

			resp, err := http.Post("http://localhost:8081/whep", "application/sdp", strings.NewReader(validOfferSDP))
			if err != nil {
				t.Fatalf("Failed to call WHEP endpoint: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("Expected status 404 for WHEP with no broadcast, got %d", resp.StatusCode)
			}

			logger.Info("WHEP no broadcast test passed")
		})

		// Test WHIP OPTIONS (CORS preflight)
		t.Run("whip_options", func(t *testing.T) {
			req, err := http.NewRequest("OPTIONS", "http://localhost:8081/whip", nil)
			if err != nil {
				t.Fatalf("Failed to create OPTIONS request: %v", err)
			}

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Failed to send OPTIONS request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200 for WHIP OPTIONS, got %d", resp.StatusCode)
			}

			// Check CORS headers
			allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
			if allowOrigin != "*" {
				t.Fatalf("Expected Access-Control-Allow-Origin: *, got %s", allowOrigin)
			}

			allowMethods := resp.Header.Get("Access-Control-Allow-Methods")
			if !strings.Contains(allowMethods, "POST") {
				t.Fatalf("Expected POST in Access-Control-Allow-Methods, got %s", allowMethods)
			}

			logger.Info("WHIP OPTIONS test passed")
		})

		// Test WHEP OPTIONS (CORS preflight)
		t.Run("whep_options", func(t *testing.T) {
			req, err := http.NewRequest("OPTIONS", "http://localhost:8081/whep", nil)
			if err != nil {
				t.Fatalf("Failed to create OPTIONS request: %v", err)
			}

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Failed to send OPTIONS request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200 for WHEP OPTIONS, got %d", resp.StatusCode)
			}

			// Check CORS headers
			allowOrigin := resp.Header.Get("Access-Control-Allow-Origin")
			if allowOrigin != "*" {
				t.Fatalf("Expected Access-Control-Allow-Origin: *, got %s", allowOrigin)
			}

			logger.Info("WHEP OPTIONS test passed")
		})

		// Test method not allowed for unsupported HTTP methods
		t.Run("unsupported_methods", func(t *testing.T) {
			endpoints := []string{"/whip", "/whep"}
			methods := []string{"GET", "PUT", "DELETE", "PATCH"}

			for _, endpoint := range endpoints {
				for _, method := range methods {
					req, err := http.NewRequest(method, "http://localhost:8081"+endpoint, nil)
					if err != nil {
						t.Fatalf("Failed to create %s request for %s: %v", method, endpoint, err)
					}

					client := &http.Client{}
					resp, err := client.Do(req)
					if err != nil {
						t.Fatalf("Failed to send %s request to %s: %v", method, endpoint, err)
					}
					resp.Body.Close()

					if resp.StatusCode != http.StatusMethodNotAllowed {
						t.Fatalf("Expected status 405 for %s %s, got %d", method, endpoint, resp.StatusCode)
					}
				}
			}

			logger.Info("Unsupported methods test passed")
		})
	})

	// Test 7: File API endpoints
	t.Run("file_api_endpoints", func(t *testing.T) {
		// Set a test directory for file serving
		testDir := "../"
		if err := apiServer.SetFileDirectory(testDir); err != nil {
			t.Fatalf("Failed to set file directory: %v", err)
		}

		// Test enqueue with server file path
		t.Run("enqueue_server_file", func(t *testing.T) {
			reqBody := map[string]interface{}{
				"file": "big_buck_bunny_1080p_h264.mov", // Relative path - should resolve against file directory
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
				t.Fatalf("Failed to enqueue server file: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("Expected status 200 for server file enqueue, got %d: %s", resp.StatusCode, string(body))
			}

			var result map[string]string
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			// The resolved file path should be absolute and include the test directory
			expectedPath := filepath.Join(testDir, "big_buck_bunny_1080p_h264.mov")
			expectedAbsPath, _ := filepath.Abs(expectedPath)
			
			if result["file"] != expectedAbsPath {
				t.Fatalf("Expected absolute file path %s, got %s", expectedAbsPath, result["file"])
			}

			logger.Info("Server file enqueue test passed", zap.String("resolved_path", result["file"]))
		})

		// Test file listing
		t.Run("list_files", func(t *testing.T) {
			resp, err := http.Get("http://localhost:8081/files")
			if err != nil {
				t.Fatalf("Failed to list files: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200 for file listing, got %d", resp.StatusCode)
			}

			var result map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("Failed to decode file listing response: %v", err)
			}

			// Validate response structure
			if _, exists := result["path"]; !exists {
				t.Fatal("Expected 'path' field in response")
			}
			if _, exists := result["files"]; !exists {
				t.Fatal("Expected 'files' field in response")
			}

			files := result["files"].([]interface{})
			logger.Info("Listed files", zap.Int("count", len(files)))

			// Should contain our test video file
			foundTestFile := false
			for _, file := range files {
				fileMap := file.(map[string]interface{})
				if strings.Contains(fileMap["name"].(string), "big_buck_bunny") {
					foundTestFile = true
					// Validate file info structure
					requiredFields := []string{"name", "path", "size", "modTime", "isDir"}
					for _, field := range requiredFields {
						if _, exists := fileMap[field]; !exists {
							t.Fatalf("Expected field %s in file info", field)
						}
					}
				}
			}
			if !foundTestFile {
				t.Fatal("Expected to find test video file in listing")
			}
		})

		// Test file listing with subdirectory
		t.Run("list_files_with_path", func(t *testing.T) {
			resp, err := http.Get("http://localhost:8081/files?path=www")
			if err != nil {
				t.Fatalf("Failed to list files with path: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200 for file listing with path, got %d", resp.StatusCode)
			}

			var result map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("Failed to decode file listing response: %v", err)
			}

			if result["path"].(string) != "www" {
				t.Fatalf("Expected path 'www', got %s", result["path"].(string))
			}

			logger.Info("Listed www directory files")
		})

		// Test directory traversal protection
		t.Run("directory_traversal_protection", func(t *testing.T) {
			maliciousPaths := []string{
				"../../../etc/passwd",
				"..%2F..%2F..%2Fetc%2Fpasswd",
				"....//....//....//etc//passwd",
			}

			for _, path := range maliciousPaths {
				resp, err := http.Get("http://localhost:8081/files?path=" + path)
				if err != nil {
					t.Fatalf("Failed to test path %s: %v", path, err)
				}
				resp.Body.Close()

				if resp.StatusCode != http.StatusForbidden {
					t.Fatalf("Expected status 403 for malicious path %s, got %d", path, resp.StatusCode)
				}
			}

			logger.Info("Directory traversal protection validated")
		})

		// Test file serving
		t.Run("serve_video_file", func(t *testing.T) {
			// Try to serve our test video file
			resp, err := http.Get("http://localhost:8081/files/big_buck_bunny_1080p_h264.mov")
			if err != nil {
				t.Fatalf("Failed to serve video file: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("Expected status 200 for video file serving, got %d", resp.StatusCode)
			}

			// Check content type (should be video)
			contentType := resp.Header.Get("Content-Type")
			if !strings.HasPrefix(contentType, "video/") && !strings.HasPrefix(contentType, "application/octet-stream") {
				logger.Info("Content type for video file", zap.String("content_type", contentType))
			}

			logger.Info("Video file served successfully")
		})

		// Test non-video file blocking
		t.Run("block_non_video_files", func(t *testing.T) {
			// Try to serve a non-video file (should be blocked)
			resp, err := http.Get("http://localhost:8081/files/go.mod")
			if err != nil {
				t.Fatalf("Failed to test non-video file serving: %v", err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusForbidden {
				t.Fatalf("Expected status 403 for non-video file, got %d", resp.StatusCode)
			}

			logger.Info("Non-video file access blocked successfully")
		})

		// Test file not found
		t.Run("file_not_found", func(t *testing.T) {
			resp, err := http.Get("http://localhost:8081/files/nonexistent.mp4")
			if err != nil {
				t.Fatalf("Failed to test nonexistent file: %v", err)
			}
			resp.Body.Close()

			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("Expected status 404 for nonexistent file, got %d", resp.StatusCode)
			}

			logger.Info("File not found handled correctly")
		})

		// Test invalid methods
		t.Run("invalid_methods", func(t *testing.T) {
			endpoints := []string{"/files", "/files/test.mp4"}
			methods := []string{"POST", "PUT", "DELETE", "PATCH"}

			for _, endpoint := range endpoints {
				for _, method := range methods {
					req, err := http.NewRequest(method, "http://localhost:8081"+endpoint, nil)
					if err != nil {
						t.Fatalf("Failed to create %s request for %s: %v", method, endpoint, err)
					}

					client := &http.Client{}
					resp, err := client.Do(req)
					if err != nil {
						t.Fatalf("Failed to send %s request to %s: %v", method, endpoint, err)
					}
					resp.Body.Close()

					if resp.StatusCode != http.StatusMethodNotAllowed {
						t.Fatalf("Expected status 405 for %s %s, got %d", method, endpoint, resp.StatusCode)
					}
				}
			}

			logger.Info("Invalid methods test passed for file endpoints")
		})
	})

	// Test 8: WebRTC status integration with streaming
	t.Run("webrtc_status_during_streaming", func(t *testing.T) {
		// Start streaming again to test WebRTC status during active streaming
		config := streammanager.Config{
			Destination: "rtmp://localhost:1936/live/test2",
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
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200, got %d", resp.StatusCode)
		}

		// Wait for streaming to start
		time.Sleep(500 * time.Millisecond)

		// Check WebRTC status while streaming is active
		resp, err = http.Get("http://localhost:8081/webrtc/status")
		if err != nil {
			t.Fatalf("Failed to get WebRTC status during streaming: %v", err)
		}
		defer resp.Body.Close()

		var status map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			t.Fatalf("Failed to decode WebRTC status: %v", err)
		}

		logger.Info("WebRTC status during streaming", zap.Any("status", status))

		// WebRTC should still be available even when RTMP streaming is active
		if _, exists := status["broadcasting"]; !exists {
			t.Fatal("Expected broadcasting field to exist")
		}

		// Stop streaming
		resp, err = http.Post("http://localhost:8081/stop", "application/json", nil)
		if err != nil {
			t.Fatalf("Failed to stop streaming: %v", err)
		}
		resp.Body.Close()

		logger.Info("WebRTC integration test completed")
	})
}
