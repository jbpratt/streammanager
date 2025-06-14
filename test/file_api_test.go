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
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

func TestFileAPI(t *testing.T) {
	// Setup test server
	logger := zaptest.NewLogger(t)
	atomicLevel := zap.NewAtomicLevelAt(zap.InfoLevel)

	apiServer, err := api.New(logger, ":8082", &atomicLevel, "/tmp/streampipe-file-test.fifo")
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}

	// Set test directory for file serving
	testDir := "../"
	if err := apiServer.SetFileDirectory(testDir); err != nil {
		t.Fatalf("Failed to set file directory: %v", err)
	}

	// Setup HTTP server
	mux := http.NewServeMux()
	apiServer.SetupRoutes(mux)

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

	// Run tests
	t.Run("TestEnqueueServerFile", testEnqueueServerFile)
	t.Run("TestListFiles", testListFiles)
	t.Run("TestListFilesWithPath", testListFilesWithPath)
	t.Run("TestDirectoryTraversalProtection", testDirectoryTraversalProtection)
	t.Run("TestServeVideoFile", testServeVideoFile)
	t.Run("TestBlockNonVideoFiles", testBlockNonVideoFiles)
	t.Run("TestFileNotFound", testFileNotFound)
	t.Run("TestInvalidMethods", testInvalidMethods)
}

func testEnqueueServerFile(t *testing.T) {
	reqBody := map[string]any{
		"file": "test/out.mp4", // Relative path - should resolve against file directory
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

	resp, err := http.Post("http://localhost:8082/enqueue", "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		t.Fatalf("Failed to enqueue server file: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200 for server file enqueue, got %d: %s", resp.StatusCode, string(body))
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// The resolved file path should be absolute and include the test directory
	testDir := "../"
	expectedPath := filepath.Join(testDir, "test/out.mp4")
	expectedAbsPath, _ := filepath.Abs(expectedPath)

	if result["file"] != expectedAbsPath {
		t.Fatalf("Expected absolute file path %s, got %s", expectedAbsPath, result["file"])
	}

	t.Logf("Server file enqueue test passed, resolved path: %s", result["file"])
}

func testListFiles(t *testing.T) {
	resp, err := http.Get("http://localhost:8082/files")
	if err != nil {
		t.Fatalf("Failed to list files: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200 for file listing, got %d", resp.StatusCode)
	}

	var result map[string]any
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

	files := result["files"].([]any)
	t.Logf("Listed %d files", len(files))

	// Should contain our test video file
	foundTestFile := false
	for _, file := range files {
		fileMap := file.(map[string]any)
		if strings.Contains(fileMap["name"].(string), "test") {
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
}

func testListFilesWithPath(t *testing.T) {
	resp, err := http.Get("http://localhost:8082/files?path=www")
	if err != nil {
		t.Fatalf("Failed to list files with path: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200 for file listing with path, got %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode file listing response: %v", err)
	}

	if result["path"].(string) != "www" {
		t.Fatalf("Expected path 'www', got %s", result["path"].(string))
	}

	t.Log("Listed www directory files successfully")
}

func testDirectoryTraversalProtection(t *testing.T) {
	maliciousPaths := []string{
		"../../../etc/passwd",
		"..%2F..%2F..%2Fetc%2Fpasswd",
		"....//....//....//etc//passwd",
	}

	for _, path := range maliciousPaths {
		resp, err := http.Get("http://localhost:8082/files?path=" + path)
		if err != nil {
			t.Fatalf("Failed to test path %s: %v", path, err)
		}
		_ = resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("Expected status 403 for malicious path %s, got %d", path, resp.StatusCode)
		}
	}

	t.Log("Directory traversal protection validated")
}

func testServeVideoFile(t *testing.T) {
	// Try to serve our test video file
	resp, err := http.Get("http://localhost:8082/files/test/out.mp4")
	if err != nil {
		t.Fatalf("Failed to serve video file: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200 for video file serving, got %d", resp.StatusCode)
	}

	// Check content type (should be video)
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "video/") && !strings.HasPrefix(contentType, "application/octet-stream") {
		t.Logf("Content type for video file: %s", contentType)
	}

	t.Log("Video file served successfully")
}

func testBlockNonVideoFiles(t *testing.T) {
	// Try to serve a non-video file (should be blocked)
	resp, err := http.Get("http://localhost:8082/files/go.mod")
	if err != nil {
		t.Fatalf("Failed to test non-video file serving: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("Expected status 403 for non-video file, got %d", resp.StatusCode)
	}

	t.Log("Non-video file access blocked successfully")
}

func testFileNotFound(t *testing.T) {
	resp, err := http.Get("http://localhost:8082/files/nonexistent.mp4")
	if err != nil {
		t.Fatalf("Failed to test nonexistent file: %v", err)
	}
	_ = resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Expected status 404 for nonexistent file, got %d", resp.StatusCode)
	}

	t.Log("File not found handled correctly")
}

func testInvalidMethods(t *testing.T) {
	endpoints := []string{"/files", "/files/test.mp4"}
	methods := []string{"POST", "PUT", "DELETE", "PATCH"}

	for _, endpoint := range endpoints {
		for _, method := range methods {
			req, err := http.NewRequest(method, "http://localhost:8082"+endpoint, nil)
			if err != nil {
				t.Fatalf("Failed to create %s request for %s: %v", method, endpoint, err)
			}

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Failed to send %s request to %s: %v", method, endpoint, err)
			}
			_ = resp.Body.Close()

			if resp.StatusCode != http.StatusMethodNotAllowed {
				t.Fatalf("Expected status 405 for %s %s, got %d", method, endpoint, resp.StatusCode)
			}
		}
	}

	t.Log("Invalid methods test passed for file endpoints")
}
