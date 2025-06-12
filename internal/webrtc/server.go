package webrtc

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
	"go.uber.org/zap"
)

type Server struct {
	logger      *zap.Logger
	api         *webrtc.API
	broadcaster *Broadcaster
	mu          sync.RWMutex
}

type Broadcaster struct {
	peerConnection *webrtc.PeerConnection
	videoTrack     *webrtc.TrackLocalStaticRTP
	audioTrack     *webrtc.TrackLocalStaticRTP
	subscribers    map[string]*webrtc.PeerConnection
	mu             sync.RWMutex
}

func NewServer(logger *zap.Logger) (*Server, error) {
	// Create a MediaEngine object to configure the supported codec
	m := &webrtc.MediaEngine{}

	// Setup the codecs you want to use
	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264, ClockRate: 90000, Channels: 0, SDPFmtpLine: "", RTCPFeedback: nil},
		PayloadType:        96,
	}, webrtc.RTPCodecTypeVideo); err != nil {
		return nil, fmt.Errorf("failed to register H264 codec: %w", err)
	}

	if err := m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus, ClockRate: 48000, Channels: 0, SDPFmtpLine: "", RTCPFeedback: nil},
		PayloadType:        111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, fmt.Errorf("failed to register Opus codec: %w", err)
	}

	// Create a InterceptorRegistry to configure interceptors
	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		return nil, fmt.Errorf("failed to register default interceptors: %w", err)
	}

	// Create the API object with the MediaEngine
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i))

	return &Server{
		logger: logger,
		api:    api,
		broadcaster: &Broadcaster{
			subscribers: make(map[string]*webrtc.PeerConnection),
		},
	}, nil
}

func (s *Server) SetupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/whip", s.handleWHIP)
	mux.HandleFunc("/whep", s.handleWHEP)
}

// WHIP endpoint - WebRTC-HTTP Ingestion Protocol for streaming to server
func (s *Server) handleWHIP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleWHIPOffer(w, r)
	case http.MethodOptions:
		s.handleWHIPOptions(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWHIPOptions(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleWHIPOffer(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Read the SDP offer
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error("Failed to read request body", zap.Error(err))
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(body),
	}

	// Create a new RTCPeerConnection
	peerConnection, err := s.api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	if err != nil {
		s.logger.Error("Failed to create peer connection", zap.Error(err))
		http.Error(w, "Failed to create peer connection", http.StatusInternalServerError)
		return
	}

	// Allow us to receive 1 video track and 1 audio track
	if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		s.logger.Error("Failed to add video transceiver", zap.Error(err))
		http.Error(w, "Failed to add video transceiver", http.StatusInternalServerError)
		return
	}

	if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		s.logger.Error("Failed to add audio transceiver", zap.Error(err))
		http.Error(w, "Failed to add audio transceiver", http.StatusInternalServerError)
		return
	}

	// Set up track handling
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		s.logger.Info("New track received",
			zap.String("codec", track.Codec().MimeType),
			zap.String("track_id", track.ID()))

		// Create local track to broadcast this stream
		var localTrack *webrtc.TrackLocalStaticRTP
		var err error

		if track.Kind() == webrtc.RTPCodecTypeVideo {
			localTrack, err = webrtc.NewTrackLocalStaticRTP(track.Codec().RTPCodecCapability, "video", "broadcast")
			if err != nil {
				s.logger.Error("Failed to create local video track", zap.Error(err))
				return
			}
			s.broadcaster.videoTrack = localTrack
		} else if track.Kind() == webrtc.RTPCodecTypeAudio {
			localTrack, err = webrtc.NewTrackLocalStaticRTP(track.Codec().RTPCodecCapability, "audio", "broadcast")
			if err != nil {
				s.logger.Error("Failed to create local audio track", zap.Error(err))
				return
			}
			s.broadcaster.audioTrack = localTrack
		}

		// Read RTP packets and forward them to all subscribers
		go s.forwardRTP(track, localTrack)
	})

	// Set the handler for Peer connection state
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		s.logger.Info("WHIP connection state changed", zap.String("state", state.String()))

		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			if s.broadcaster.peerConnection == peerConnection {
				s.broadcaster.peerConnection = nil
				s.broadcaster.videoTrack = nil
				s.broadcaster.audioTrack = nil
			}
		}
	})

	// Set the remote SessionDescription
	if err = peerConnection.SetRemoteDescription(offer); err != nil {
		s.logger.Error("Failed to set remote description", zap.Error(err))
		http.Error(w, "Failed to set remote description", http.StatusBadRequest)
		return
	}

	// Create answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		s.logger.Error("Failed to create answer", zap.Error(err))
		http.Error(w, "Failed to create answer", http.StatusInternalServerError)
		return
	}

	// Create a datachannel with label 'data'
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	// Sets the LocalDescription, and starts our UDP listeners
	if err = peerConnection.SetLocalDescription(answer); err != nil {
		s.logger.Error("Failed to set local description", zap.Error(err))
		http.Error(w, "Failed to set local description", http.StatusInternalServerError)
		return
	}

	// Block until ICE Gathering is complete, disabling trickle ICE
	<-gatherComplete

	s.broadcaster.peerConnection = peerConnection

	// Send the answer back
	w.Header().Set("Content-Type", "application/sdp")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprint(w, peerConnection.LocalDescription().SDP)

	s.logger.Info("WHIP connection established")
}

// WHEP endpoint - WebRTC-HTTP Egress Protocol for playing from server
func (s *Server) handleWHEP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.handleWHEPOffer(w, r)
	case http.MethodOptions:
		s.handleWHEPOptions(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleWHEPOptions(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleWHEPOffer(w http.ResponseWriter, r *http.Request) {
	s.broadcaster.mu.RLock()
	if s.broadcaster.videoTrack == nil && s.broadcaster.audioTrack == nil {
		s.broadcaster.mu.RUnlock()
		http.Error(w, "No active broadcast", http.StatusNotFound)
		return
	}
	s.broadcaster.mu.RUnlock()

	// Read the SDP offer
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error("Failed to read WHEP request body", zap.Error(err))
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(body),
	}

	// Create a new RTCPeerConnection for the subscriber
	peerConnection, err := s.api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	})
	if err != nil {
		s.logger.Error("Failed to create WHEP peer connection", zap.Error(err))
		http.Error(w, "Failed to create peer connection", http.StatusInternalServerError)
		return
	}

	// Add tracks to the peer connection
	s.broadcaster.mu.RLock()
	if s.broadcaster.videoTrack != nil {
		if _, err = peerConnection.AddTrack(s.broadcaster.videoTrack); err != nil {
			s.broadcaster.mu.RUnlock()
			s.logger.Error("Failed to add video track to WHEP connection", zap.Error(err))
			http.Error(w, "Failed to add video track", http.StatusInternalServerError)
			return
		}
	}

	if s.broadcaster.audioTrack != nil {
		if _, err = peerConnection.AddTrack(s.broadcaster.audioTrack); err != nil {
			s.broadcaster.mu.RUnlock()
			s.logger.Error("Failed to add audio track to WHEP connection", zap.Error(err))
			http.Error(w, "Failed to add audio track", http.StatusInternalServerError)
			return
		}
	}
	s.broadcaster.mu.RUnlock()

	// Generate a unique ID for this subscriber
	subscriberID := fmt.Sprintf("subscriber_%d", time.Now().UnixNano())

	// Set the handler for Peer connection state
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		s.logger.Info("WHEP connection state changed",
			zap.String("subscriber_id", subscriberID),
			zap.String("state", state.String()))

		if state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
			s.broadcaster.mu.Lock()
			delete(s.broadcaster.subscribers, subscriberID)
			s.broadcaster.mu.Unlock()
		}
	})

	// Set the remote SessionDescription
	if err = peerConnection.SetRemoteDescription(offer); err != nil {
		s.logger.Error("Failed to set WHEP remote description", zap.Error(err))
		http.Error(w, "Failed to set remote description", http.StatusBadRequest)
		return
	}

	// Create answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		s.logger.Error("Failed to create WHEP answer", zap.Error(err))
		http.Error(w, "Failed to create answer", http.StatusInternalServerError)
		return
	}

	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	// Sets the LocalDescription, and starts our UDP listeners
	if err = peerConnection.SetLocalDescription(answer); err != nil {
		s.logger.Error("Failed to set WHEP local description", zap.Error(err))
		http.Error(w, "Failed to set local description", http.StatusInternalServerError)
		return
	}

	// Block until ICE Gathering is complete
	<-gatherComplete

	// Add to subscribers list
	s.broadcaster.mu.Lock()
	s.broadcaster.subscribers[subscriberID] = peerConnection
	s.broadcaster.mu.Unlock()

	// Send the answer back
	w.Header().Set("Content-Type", "application/sdp")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusCreated)
	fmt.Fprint(w, peerConnection.LocalDescription().SDP)

	s.logger.Info("WHEP connection established", zap.String("subscriber_id", subscriberID))
}

func (s *Server) forwardRTP(remoteTrack *webrtc.TrackRemote, localTrack *webrtc.TrackLocalStaticRTP) {
	rtpBuf := make([]byte, 1400)
	for {
		i, _, err := remoteTrack.Read(rtpBuf)
		if err != nil {
			s.logger.Debug("Track read error", zap.Error(err))
			return
		}

		// Write to local track to forward to all subscribers
		if _, err = localTrack.Write(rtpBuf[:i]); err != nil {
			s.logger.Debug("Track write error", zap.Error(err))
			return
		}
	}
}

func (s *Server) GetStatus() map[string]interface{} {
	s.broadcaster.mu.RLock()
	defer s.broadcaster.mu.RUnlock()

	return map[string]interface{}{
		"broadcasting":      s.broadcaster.peerConnection != nil,
		"subscribers_count": len(s.broadcaster.subscribers),
		"has_video":         s.broadcaster.videoTrack != nil,
		"has_audio":         s.broadcaster.audioTrack != nil,
	}
}
