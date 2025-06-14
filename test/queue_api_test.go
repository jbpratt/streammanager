package test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/jbpratt/streammanager/internal/api"
	"github.com/jbpratt/streammanager/internal/rtmp"
	"github.com/jbpratt/streammanager/internal/streammanager"
	"github.com/jbpratt/streammanager/internal/webrtc"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

type testSetup struct {
	logger         *zap.Logger
	destRTMPServer *rtmp.Server
	apiServer      *api.Server
	webrtcServer   *webrtc.Server
	httpServer     *http.Server
	testFile       string
	httpPort       int
	rtmpPort       int
	cleanup        func()
}

func setupTestEnvironment(t *testing.T, portOffset int) *testSetup {
	logger := zaptest.NewLogger(t)

	// Calculate unique ports for this test
	rtmpPort := 1936 + portOffset
	apiRTMPPort := 1937 + portOffset
	httpPort := 8081 + portOffset

	// Start destination RTMP server (where we'll stream TO)
	destRTMPAddr := fmt.Sprintf(":%d", rtmpPort)
	destRTMPServer, err := rtmp.NewServer(logger, destRTMPAddr)
	if err != nil {
		t.Fatalf("Failed to create destination RTMP server: %v", err)
	}

	go func() {
		if err := destRTMPServer.Start(); err != nil {
			logger.Error("Destination RTMP server error", zap.Error(err))
		}
	}()

	// Give RTMP server time to start
	time.Sleep(100 * time.Millisecond)

	// Create API server with embedded RTMP server
	atomicLevel := zap.NewAtomicLevelAt(zap.InfoLevel)
	apiRTMPAddr := fmt.Sprintf(":%d", apiRTMPPort)
	fifoPath := fmt.Sprintf("/tmp/streampipe-test-%d.fifo", portOffset)
	apiServer, err := api.New(logger, apiRTMPAddr, &atomicLevel, fifoPath)
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

	httpAddr := fmt.Sprintf(":%d", httpPort)
	httpServer := &http.Server{
		Addr:    httpAddr,
		Handler: mux,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", zap.Error(err))
		}
	}()

	// Give HTTP server time to start
	time.Sleep(100 * time.Millisecond)

	// Get absolute path to test file
	testFile, err := filepath.Abs("out.mp4")
	if err != nil {
		t.Fatalf("Failed to get absolute path to test file: %v", err)
	}

	cleanup := func() {
		// Stop HTTP server
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Error("Failed to shutdown HTTP server", zap.Error(err))
		}

		// Stop RTMP server
		if err := destRTMPServer.Stop(); err != nil {
			logger.Error("Failed to stop destination RTMP server", zap.Error(err))
		}
	}

	return &testSetup{
		logger:         logger,
		destRTMPServer: destRTMPServer,
		apiServer:      apiServer,
		webrtcServer:   webrtcServer,
		httpServer:     httpServer,
		testFile:       testFile,
		httpPort:       httpPort,
		rtmpPort:       rtmpPort,
		cleanup:        cleanup,
	}
}

func TestEnqueueFile(t *testing.T) {
	setup := setupTestEnvironment(t, 0)
	defer setup.cleanup()

	reqBody := map[string]any{
		"file": setup.testFile,
		"overlay": map[string]any{
			"showFilename": true,
			"position":     "bottom-right",
			"fontSize":     24,
		},
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	url := fmt.Sprintf("http://localhost:%d/enqueue", setup.httpPort)
	resp, err := http.Post(url, "application/json", bytes.NewReader(reqJSON))
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

	if result["file"] != setup.testFile {
		t.Fatalf("Expected file %s, got %s", setup.testFile, result["file"])
	}

	setup.logger.Info("Enqueued file with ID", zap.String("id", result["id"]))
}

func TestQueueStatus(t *testing.T) {
	setup := setupTestEnvironment(t, 1)
	defer setup.cleanup()

	// First enqueue a file
	reqBody := map[string]any{
		"file": setup.testFile,
		"overlay": map[string]any{
			"showFilename": true,
			"position":     "bottom-right",
			"fontSize":     24,
		},
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	enqueueURL := fmt.Sprintf("http://localhost:%d/enqueue", setup.httpPort)
	resp, err := http.Post(enqueueURL, "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		t.Fatalf("Failed to enqueue file: %v", err)
	}
	resp.Body.Close()

	// Start streaming to trigger queue processing
	config := streammanager.Config{
		Destination: fmt.Sprintf("rtmp://localhost:%d/live/test", setup.rtmpPort),
		RTMPAddr:    fmt.Sprintf(":%d", setup.rtmpPort+1),
		Encoder:     "libx264",
		Preset:      "ultrafast",
		LogLevel:    "warning",
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	startURL := fmt.Sprintf("http://localhost:%d/start", setup.httpPort)
	resp, err = http.Post(startURL, "application/json", bytes.NewReader(configJSON))
	if err != nil {
		t.Fatalf("Failed to start streaming: %v", err)
	}
	resp.Body.Close()

	// Give streaming time to start
	time.Sleep(2 * time.Second)

	// Check queue status
	queueURL := fmt.Sprintf("http://localhost:%d/queue", setup.httpPort)
	resp, err = http.Get(queueURL)
	if err != nil {
		t.Fatalf("Failed to get queue status: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var queueStatus map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&queueStatus); err != nil {
		t.Fatalf("Failed to decode queue status: %v", err)
	}

	status := queueStatus["status"].(map[string]any)

	if !status["running"].(bool) {
		t.Fatal("Expected stream manager to be running")
	}

	setup.logger.Info("Stream manager status", zap.Any("status", status))

	// Check if file is being processed
	if playing, exists := status["playing"]; exists {
		playingMap := playing.(map[string]any)
		setup.logger.Info("Currently playing file",
			zap.String("file", playingMap["file"].(string)),
			zap.String("id", playingMap["id"].(string)))
	}

	// Stop streaming
	stopURL := fmt.Sprintf("http://localhost:%d/stop", setup.httpPort)
	http.Post(stopURL, "application/json", nil)
}

func TestProgressTracking(t *testing.T) {
	setup := setupTestEnvironment(t, 2)
	defer setup.cleanup()

	// First enqueue a file
	reqBody := map[string]any{
		"file": setup.testFile,
		"overlay": map[string]any{
			"showFilename": true,
			"position":     "bottom-right",
			"fontSize":     24,
		},
	}

	reqJSON, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	enqueueURL := fmt.Sprintf("http://localhost:%d/enqueue", setup.httpPort)
	resp, err := http.Post(enqueueURL, "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		t.Fatalf("Failed to enqueue file: %v", err)
	}
	resp.Body.Close()

	// Start streaming to trigger progress tracking
	config := streammanager.Config{
		Destination: fmt.Sprintf("rtmp://localhost:%d/live/test", setup.rtmpPort),
		RTMPAddr:    fmt.Sprintf(":%d", setup.rtmpPort+1),
		Encoder:     "libx264",
		Preset:      "ultrafast",
		LogLevel:    "warning",
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	startURL := fmt.Sprintf("http://localhost:%d/start", setup.httpPort)
	resp, err = http.Post(startURL, "application/json", bytes.NewReader(configJSON))
	if err != nil {
		t.Fatalf("Failed to start streaming: %v", err)
	}
	resp.Body.Close()

	// Give streaming time to start
	time.Sleep(2 * time.Second)

	// Enhanced progress validation
	progressUpdates := 0
	maxChecks := 20
	var lastPercentage float64 = -1
	var lastFrame int64 = -1
	percentageIncreased := false
	frameIncreased := false

	for range maxChecks {
		progressURL := fmt.Sprintf("http://localhost:%d/progress", setup.httpPort)
		resp, err := http.Get(progressURL)
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

			// Validate required progress data fields
			requiredFields := []string{"frame", "fps", "timestamp", "duration", "percentage"}
			for _, field := range requiredFields {
				if _, ok := progress[field]; !ok {
					t.Fatalf("Expected %s field in progress data", field)
				}
			}

			// Extract and validate progress values
			frame := int64(progress["frame"].(float64))
			fps := progress["fps"].(float64)
			duration := progress["duration"].(float64)
			percentage := progress["percentage"].(float64)

			// Validate data types and ranges
			if frame < 0 {
				t.Fatal("Frame count should not be negative")
			}
			if fps < 0 {
				t.Fatal("FPS should not be negative")
			}
			if duration < 0 {
				t.Fatal("Duration should not be negative")
			}
			if percentage < 0 || percentage > 100 {
				t.Fatalf("Percentage should be between 0-100, got %.2f", percentage)
			}

			// Check if values are increasing (indicating progress)
			if lastPercentage >= 0 && percentage > lastPercentage {
				percentageIncreased = true
				setup.logger.Info("Progress percentage increased",
					zap.Float64("from", lastPercentage),
					zap.Float64("to", percentage))
			}
			if lastFrame >= 0 && frame > lastFrame {
				frameIncreased = true
			}

			// Log comprehensive progress details
			setup.logger.Info("Progress update received",
				zap.Int64("frame", frame),
				zap.Float64("fps", fps),
				zap.String("bitrate", progress["bitrate"].(string)),
				zap.String("out_time", progress["out_time"].(string)),
				zap.String("speed", progress["speed"].(string)),
				zap.Float64("duration", duration),
				zap.Float64("percentage", percentage))

			lastPercentage = percentage
			lastFrame = frame

			// If we've seen good progress data, we can break early
			if progressUpdates >= 3 && (percentageIncreased || frameIncreased) {
				break
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	if progressUpdates == 0 {
		t.Fatal("Expected to receive at least one progress update")
	}

	// Validate that we're seeing actual progress
	if progressUpdates >= 2 && !percentageIncreased && !frameIncreased {
		setup.logger.Warn("Progress values did not increase - may indicate very short video or processing completed quickly")
	}

	setup.logger.Info("Enhanced progress validation completed",
		zap.Int("total_updates", progressUpdates),
		zap.Bool("percentage_increased", percentageIncreased),
		zap.Bool("frame_increased", frameIncreased))

	// Let it process for 5 more seconds after progress check
	setup.logger.Info("Letting file process for 5 more seconds")
	time.Sleep(5 * time.Second)

	// Stop streaming
	stopURL := fmt.Sprintf("http://localhost:%d/stop", setup.httpPort)
	resp, err = http.Post(stopURL, "application/json", nil)
	if err != nil {
		t.Fatalf("Failed to stop streaming: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	setup.logger.Info("Successfully stopped streaming")

	// Check final status
	time.Sleep(1 * time.Second)
	finalQueueURL := fmt.Sprintf("http://localhost:%d/queue", setup.httpPort)
	resp, err = http.Get(finalQueueURL)
	if err != nil {
		t.Fatalf("Failed to get final queue status: %v", err)
	}
	defer resp.Body.Close()

	var queueStatus map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&queueStatus); err != nil {
		t.Fatalf("Failed to decode final queue status: %v", err)
	}

	status := queueStatus["status"].(map[string]any)

	if status["running"].(bool) {
		setup.logger.Info("Stream manager is still running (may be shutting down)")
	} else {
		setup.logger.Info("Stream manager has stopped")
	}

	setup.logger.Info("Final status", zap.Any("status", status))
}
