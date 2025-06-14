package test

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/jbpratt/streammanager/internal/api"
	"github.com/jbpratt/streammanager/internal/rtmp"
	"github.com/jbpratt/streammanager/internal/webrtc"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	// Test server ports
	TestRTMPDestPort   = ":1936"
	TestRTMPSourcePort = ":1937"
	TestHTTPPort       = ":8081"

	// Test file path
	TestFIFOPath = "/tmp/streampipe-test.fifo"
)

// SetupRTMPDestServer creates and starts the destination RTMP server on port 1936
func SetupRTMPDestServer(t *testing.T, logger *zap.Logger) (*rtmp.Server, func()) {
	destRTMPServer, err := rtmp.NewServer(logger, TestRTMPDestPort)
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

	cleanup := func() {
		if err := destRTMPServer.Stop(); err != nil {
			logger.Error("Failed to stop destination RTMP server", zap.Error(err))
		}
	}

	return destRTMPServer, cleanup
}

// SetupAPIServer creates the API server with WebRTC server integration on port 8081
func SetupAPIServer(t *testing.T, logger *zap.Logger) (*api.Server, *webrtc.Server, *http.Server, func()) {
	// Create API server with embedded RTMP server
	atomicLevel := zap.NewAtomicLevelAt(zap.InfoLevel)
	apiServer, err := api.New(logger, TestRTMPSourcePort, &atomicLevel, TestFIFOPath)
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
		Addr:    TestHTTPPort,
		Handler: mux,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", zap.Error(err))
		}
	}()

	// Give HTTP server time to start
	time.Sleep(100 * time.Millisecond)

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Error("Failed to shutdown HTTP server", zap.Error(err))
		}
	}

	return apiServer, webrtcServer, httpServer, cleanup
}

// SetupTestEnvironment combines both RTMP destination and API server setups
func SetupTestEnvironment(t *testing.T) (*zap.Logger, *rtmp.Server, *api.Server, *webrtc.Server, *http.Server, func()) {
	// Create test logger
	logger := zaptest.NewLogger(t)

	// Setup RTMP destination server
	destRTMPServer, rtmpCleanup := SetupRTMPDestServer(t, logger)

	// Setup API server
	apiServer, webrtcServer, httpServer, apiCleanup := SetupAPIServer(t, logger)

	// Combined cleanup function
	cleanup := func() {
		apiCleanup()
		rtmpCleanup()
	}

	return logger, destRTMPServer, apiServer, webrtcServer, httpServer, cleanup
}

// GetTestFile returns the absolute path to test/out.mp4
func GetTestFile(t *testing.T) string {
	testFile, err := filepath.Abs("out.mp4")
	if err != nil {
		t.Fatalf("Failed to get absolute path to test file: %v", err)
	}
	return testFile
}
