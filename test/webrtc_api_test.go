package test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jbpratt/streammanager/internal/api"
	"github.com/jbpratt/streammanager/internal/rtmp"
	"github.com/jbpratt/streammanager/internal/webrtc"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// setupWebRTCTestServer creates and starts a test server with WebRTC support
func setupWebRTCTestServer(t *testing.T) (*http.Server, func()) {
	t.Helper()

	// Create test logger
	logger := zaptest.NewLogger(t)

	// Start destination RTMP server
	destRTMPServer, err := rtmp.NewServer(logger, ":1938") // Use different port to avoid conflicts
	if err != nil {
		t.Fatalf("Failed to create destination RTMP server: %v", err)
	}

	go func() {
		if err := destRTMPServer.Start(); err != nil {
			logger.Error("Destination RTMP server error", zap.Error(err))
		}
	}()

	// Create API server with embedded RTMP server
	atomicLevel := zap.NewAtomicLevelAt(zap.InfoLevel)
	apiServer, err := api.New(logger, ":1939", &atomicLevel, "/tmp/streampipe-webrtc-test.fifo")
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
		Addr:    ":8082", // Use different port to avoid conflicts
		Handler: mux,
	}

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("HTTP server error", zap.Error(err))
		}
	}()

	// Give servers time to start
	time.Sleep(200 * time.Millisecond)

	// Cleanup function
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(ctx); err != nil {
			logger.Error("Failed to shutdown HTTP server", zap.Error(err))
		}
		if err := destRTMPServer.Stop(); err != nil {
			logger.Error("Failed to stop destination RTMP server", zap.Error(err))
		}
	}

	return httpServer, cleanup
}

func TestWebRTCStatus(t *testing.T) {
	_, cleanup := setupWebRTCTestServer(t)
	defer cleanup()

	t.Run("status_endpoint_validation", func(t *testing.T) {
		resp, err := http.Get("http://localhost:8082/webrtc/status")
		if err != nil {
			t.Fatalf("Failed to get WebRTC status: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected status 200 for WebRTC status, got %d", resp.StatusCode)
		}

		var status map[string]any
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
	})

	t.Run("status_content_type", func(t *testing.T) {
		resp, err := http.Get("http://localhost:8082/webrtc/status")
		if err != nil {
			t.Fatalf("Failed to get WebRTC status: %v", err)
		}
		defer resp.Body.Close()

		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "application/json") {
			t.Fatalf("Expected JSON content type, got %s", contentType)
		}
	})
}

func TestWHIPInvalidSDP(t *testing.T) {
	_, cleanup := setupWebRTCTestServer(t)
	defer cleanup()

	testCases := []struct {
		name        string
		sdpContent  string
		description string
	}{
		{
			name:        "completely_invalid_sdp",
			sdpContent:  "invalid sdp content",
			description: "Test with completely invalid SDP content",
		},
		{
			name:        "empty_sdp",
			sdpContent:  "",
			description: "Test with empty SDP content",
		},
		{
			name:        "malformed_sdp",
			sdpContent:  "v=0\no=malformed",
			description: "Test with malformed SDP structure",
		},
		{
			name:        "json_instead_of_sdp",
			sdpContent:  `{"type": "offer", "sdp": "invalid"}`,
			description: "Test with JSON instead of SDP",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post("http://localhost:8082/whip", "application/sdp", strings.NewReader(tc.sdpContent))
			if err != nil {
				t.Fatalf("Failed to call WHIP endpoint: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("Expected status 400 for invalid SDP (%s), got %d", tc.description, resp.StatusCode)
			}
		})
	}
}

func TestWHEPNoBroadcast(t *testing.T) {
	_, cleanup := setupWebRTCTestServer(t)
	defer cleanup()

	t.Run("no_active_broadcast", func(t *testing.T) {
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

		resp, err := http.Post("http://localhost:8082/whep", "application/sdp", strings.NewReader(validOfferSDP))
		if err != nil {
			t.Fatalf("Failed to call WHEP endpoint: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("Expected status 404 for WHEP with no broadcast, got %d", resp.StatusCode)
		}
	})

	t.Run("verify_status_shows_no_broadcast", func(t *testing.T) {
		resp, err := http.Get("http://localhost:8082/webrtc/status")
		if err != nil {
			t.Fatalf("Failed to get WebRTC status: %v", err)
		}
		defer resp.Body.Close()

		var status map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			t.Fatalf("Failed to decode WebRTC status: %v", err)
		}

		if status["broadcasting"].(bool) {
			t.Fatal("Expected broadcasting to be false when no broadcast is active")
		}
	})
}

func TestWHIPOptions(t *testing.T) {
	_, cleanup := setupWebRTCTestServer(t)
	defer cleanup()

	t.Run("cors_preflight", func(t *testing.T) {
		req, err := http.NewRequest("OPTIONS", "http://localhost:8082/whip", nil)
		if err != nil {
			t.Fatalf("Failed to create OPTIONS request: %v", err)
		}

		// Add preflight request headers
		req.Header.Set("Origin", "https://example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "Content-Type")

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
	})

	t.Run("options_without_origin", func(t *testing.T) {
		req, err := http.NewRequest("OPTIONS", "http://localhost:8082/whip", nil)
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
			t.Fatalf("Expected status 200 for WHIP OPTIONS without origin, got %d", resp.StatusCode)
		}
	})
}

func TestWHEPOptions(t *testing.T) {
	_, cleanup := setupWebRTCTestServer(t)
	defer cleanup()

	t.Run("cors_preflight", func(t *testing.T) {
		req, err := http.NewRequest("OPTIONS", "http://localhost:8082/whep", nil)
		if err != nil {
			t.Fatalf("Failed to create OPTIONS request: %v", err)
		}

		// Add preflight request headers
		req.Header.Set("Origin", "https://example.com")
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "Content-Type")

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
	})

	t.Run("options_response_headers", func(t *testing.T) {
		req, err := http.NewRequest("OPTIONS", "http://localhost:8082/whep", nil)
		if err != nil {
			t.Fatalf("Failed to create OPTIONS request: %v", err)
		}

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to send OPTIONS request: %v", err)
		}
		defer resp.Body.Close()

		// Verify that appropriate headers are set for WHEP
		allowHeaders := resp.Header.Get("Access-Control-Allow-Headers")
		if allowHeaders != "" && !strings.Contains(allowHeaders, "Content-Type") {
			t.Fatalf("Expected Content-Type in Access-Control-Allow-Headers, got %s", allowHeaders)
		}
	})
}

func TestUnsupportedMethods(t *testing.T) {
	_, cleanup := setupWebRTCTestServer(t)
	defer cleanup()

	endpoints := []string{"/whip", "/whep"}
	unsupportedMethods := []string{"GET", "PUT", "DELETE", "PATCH"}

	for _, endpoint := range endpoints {
		for _, method := range unsupportedMethods {
			t.Run(method+"_"+strings.TrimPrefix(endpoint, "/"), func(t *testing.T) {
				req, err := http.NewRequest(method, "http://localhost:8082"+endpoint, nil)
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
			})
		}
	}
}

func TestWebRTCEndpointsIntegration(t *testing.T) {
	_, cleanup := setupWebRTCTestServer(t)
	defer cleanup()

	t.Run("all_endpoints_available", func(t *testing.T) {
		endpoints := []string{
			"/webrtc/status",
			"/whip",
			"/whep",
		}

		for _, endpoint := range endpoints {
			// Test that endpoints exist (won't return 404)
			if endpoint == "/webrtc/status" {
				resp, err := http.Get("http://localhost:8082" + endpoint)
				if err != nil {
					t.Fatalf("Failed to call %s: %v", endpoint, err)
				}
				resp.Body.Close()

				if resp.StatusCode == http.StatusNotFound {
					t.Fatalf("Endpoint %s not found", endpoint)
				}
			} else {
				// For WHIP/WHEP, test with OPTIONS to avoid triggering actual WebRTC logic
				req, err := http.NewRequest("OPTIONS", "http://localhost:8082"+endpoint, nil)
				if err != nil {
					t.Fatalf("Failed to create OPTIONS request for %s: %v", endpoint, err)
				}

				client := &http.Client{}
				resp, err := client.Do(req)
				if err != nil {
					t.Fatalf("Failed to send OPTIONS request to %s: %v", endpoint, err)
				}
				resp.Body.Close()

				if resp.StatusCode == http.StatusNotFound {
					t.Fatalf("Endpoint %s not found", endpoint)
				}
			}
		}
	})

	t.Run("content_type_validation", func(t *testing.T) {
		// WHIP should only accept application/sdp
		req, err := http.NewRequest("POST", "http://localhost:8082/whip", strings.NewReader("test"))
		if err != nil {
			t.Fatalf("Failed to create POST request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json") // Wrong content type

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Failed to send POST request: %v", err)
		}
		resp.Body.Close()

		// Should reject non-SDP content type
		if resp.StatusCode == http.StatusOK {
			t.Fatal("Expected WHIP to reject non-SDP content type")
		}
	})
}
