package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jbpratt/streammanager/internal/api"
	"github.com/jbpratt/streammanager/internal/webrtc"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TODO: multistream to a local server to provide a preview
// TODO: allow setting subtitles in the overlay options
// TODO: improve the fps in the progress to not estimate total frames instead using ffprobe to calculate
//       ffprobe -v quiet -select_streams v:0 -show_entries stream=nb_frames,duration,r_frame_rate -of json

func main() {
	addr := flag.String("http-addr", ":8080", "server address")
	rtmpAddr := flag.String("rtmp-addr", ":1935", "RTMP server address")
	logLevel := flag.String("log-level", "info", "Log level (debug, info)")
	fileDir := flag.String("file-dir", ".", "Directory to serve files from")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var level zapcore.Level
	if err := level.UnmarshalText([]byte(strings.ToLower(*logLevel))); err != nil {
		log.Fatalf("Invalid log level: %v", err)
	}

	// Create atomic level for runtime changes
	atomicLevel := zap.NewAtomicLevelAt(level)
	
	logger, err := zap.NewDevelopment(zap.IncreaseLevel(atomicLevel))
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = logger.Sync() // Safe to ignore error in defer during shutdown
	}()

	apiServer, err := api.New(logger, *rtmpAddr, &atomicLevel)
	if err != nil {
		logger.Fatal("Failed to create API server", zap.Error(err))
	}

	// Set file directory for file serving
	if err := apiServer.SetFileDirectory(*fileDir); err != nil {
		logger.Fatal("Failed to set file directory", zap.Error(err))
	}

	webrtcServer, err := webrtc.NewServer(logger)
	if err != nil {
		logger.Fatal("Failed to create WebRTC server", zap.Error(err))
	}

	// Connect WebRTC server to API for status reporting
	apiServer.SetWebRTCServer(webrtcServer)

	mux := http.NewServeMux()

	mux.Handle("/", http.FileServer(http.Dir("www")))
	apiServer.SetupRoutes(mux)
	webrtcServer.SetupRoutes(mux)

	srvr := &http.Server{
		Addr:    *addr,
		Handler: mux,
	}

	errC := make(chan error, 1)
	go func() {
		logger.Info("Starting HTTP server", zap.String("addr", *addr))
		if err := srvr.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errC <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("Shutting down HTTP server")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srvr.Shutdown(ctx); err != nil {
			logger.Fatal("Failed to shutdown HTTP server", zap.Error(err))
		}
	case err := <-errC:
		if err != nil {
			logger.Fatal("HTTP server error", zap.Error(err))
		}
	}
}
