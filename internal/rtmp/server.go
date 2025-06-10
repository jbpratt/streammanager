package rtmp

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path"

	"github.com/MemeLabs/strims/pkg/rtmpingress"
	"go.uber.org/zap"
)

type Server struct {
	server    *rtmpingress.Server
	logger    *zap.Logger
	tsfolders []string
}

type tw struct {
	i    int
	path string
	file io.WriteCloser
}

func newTw() *tw {
	tmp, err := os.MkdirTemp("", "streammanager")
	if err != nil {
		panic(err)
	}
	return &tw{path: tmp}
}

func (t *tw) Write(p []byte) (int, error) {
	if t.file == nil {
		f, err := os.Create(path.Join(t.path, fmt.Sprintf("%d.mp4", t.i)))
		if err != nil {
			return 0, err
		}
		t.file = f
		t.i++

		// trim moov atom length header
		p = p[2:]
	}

	n, err := t.file.Write(p)
	if err != nil {
		return 0, err
	}

	return n, nil
}

func (t *tw) Flush() error {
	if err := t.file.Close(); err != nil {
		return err
	}

	t.file = nil

	return nil
}

func (t *tw) Close() error {
	return nil
}

func NewServer(logger *zap.Logger, addr string) (*Server, error) {
	s := &Server{
		logger:    logger,
		tsfolders: make([]string, 0),
	}

	transcoder := rtmpingress.NewTranscoder(logger)

	s.server = &rtmpingress.Server{
		Addr:        addr,
		Logger:      logger,
		CheckOrigin: func(addr *rtmpingress.StreamAddr, conn *rtmpingress.Conn) bool { return true },
		HandleStream: func(a *rtmpingress.StreamAddr, c *rtmpingress.Conn) {
			tw := newTw()
			go func() {
				if err := transcoder.Transcode(c.Context(), a.URI, a.Key, "source", tw); err != nil {
					logger.Error("transcoding", zap.Error(err))
				}
			}()
			s.tsfolders = append(s.tsfolders, tw.path)
		},
		BaseContext: func(nc net.Conn) context.Context {
			return context.Background()
		},
	}

	return s, nil
}

func (s *Server) Start() error {
	s.logger.Info("Starting RTMP server", zap.String("addr", s.server.Addr))
	return s.server.Listen()
}

func (s *Server) Stop() error {
	s.logger.Info("Stopping RTMP server")
	return s.server.Close()
}
