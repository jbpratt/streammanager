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
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// TODO: multistream to a local server to provide a preview
// TODO: allow setting subtitles in the overlay options

func main() {
	addr := flag.String("http-addr", ":8080", "server address")
	rtmpAddr := flag.String("rtmp-addr", ":1935", "RTMP server address")
	logLevel := flag.String("log-level", "info", "Log level (debug, info)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var level zapcore.Level
	if err := level.UnmarshalText([]byte(strings.ToLower(*logLevel))); err != nil {
		log.Fatalf("Invalid log level: %v", err)
	}

	logger, err := zap.NewDevelopment(zap.IncreaseLevel(level))
	if err != nil {
		panic(err)
	}
	defer logger.Sync()

	apiServer, err := api.New(logger, *rtmpAddr)
	if err != nil {
		logger.Fatal("Failed to create API server", zap.Error(err))
	}

	mux := http.NewServeMux()

	mux.Handle("/", http.FileServer(http.Dir("www")))
	apiServer.SetupRoutes(mux)

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
