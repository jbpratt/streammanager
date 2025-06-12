export default class WebRTCManager {
  constructor() {
    this.whepConnection = null;
    this.isWhepConnected = false;
    this.webrtcStatusInterval = null;
  }

  async connect() {
    if (this.isWhepConnected) {
      return { success: false, message: "Already connected" };
    }

    try {
      // Check if there's an active broadcast first
      const statusResponse = await fetch("/webrtc/status");
      const status = await statusResponse.json();

      if (!status.broadcasting && !status.has_video && !status.has_audio) {
        return {
          success: false,
          message: "No active broadcast available",
          status: "no-broadcast",
        };
      }

      // Create RTCPeerConnection
      this.whepConnection = new RTCPeerConnection({
        iceServers: [
          { urls: "stun:stun.l.google.com:19302" },
        ],
      });

      // Set up event handlers
      this.setupConnectionHandlers();

      // Create offer
      const offer = await this.whepConnection.createOffer();
      await this.whepConnection.setLocalDescription(offer);

      // Wait for ICE gathering to complete
      await this.waitForICEGathering();

      // Send offer to WHEP endpoint
      const response = await fetch("/whep", {
        method: "POST",
        headers: {
          "Content-Type": "application/sdp",
        },
        body: this.whepConnection.localDescription.sdp,
      });

      if (!response.ok) {
        const errorText = await response.text();
        throw new Error(
          `WHEP request failed: ${response.status} - ${errorText}`,
        );
      }

      const answerSDP = await response.text();
      await this.whepConnection.setRemoteDescription({
        type: "answer",
        sdp: answerSDP,
      });

      return {
        success: true,
        message: "Connection initiated",
        status: "connecting",
      };
    } catch (error) {
      console.error("WHEP connection error:", error);
      this.disconnect();
      return {
        success: false,
        message: `Connection failed: ${error.message}`,
        status: "failed",
      };
    }
  }

  disconnect() {
    if (this.whepConnection) {
      this.whepConnection.close();
      this.whepConnection = null;
    }

    this.isWhepConnected = false;
    return { success: true, message: "Disconnected" };
  }

  setupConnectionHandlers() {
    if (!this.whepConnection) return;

    // Handle incoming tracks
    this.whepConnection.ontrack = (event) => {
      console.log("Received track:", event.track.kind);
      this.onTrackReceived?.(event);
    };

    // Handle connection state changes
    this.whepConnection.onconnectionstatechange = () => {
      const state = this.whepConnection.connectionState;
      console.log("Connection state:", state);

      if (state === "connected") {
        this.isWhepConnected = true;
      } else if (state === "failed" || state === "disconnected") {
        this.disconnect();
      }

      this.onConnectionStateChange?.(state);
    };

    // Handle ICE connection state changes
    this.whepConnection.oniceconnectionstatechange = () => {
      console.log(
        "ICE connection state:",
        this.whepConnection.iceConnectionState,
      );
      this.onICEConnectionStateChange?.(this.whepConnection.iceConnectionState);
    };
  }

  waitForICEGathering() {
    return new Promise((resolve) => {
      if (this.whepConnection.iceGatheringState === "complete") {
        resolve();
      } else {
        this.whepConnection.addEventListener("icegatheringstatechange", () => {
          if (this.whepConnection.iceGatheringState === "complete") {
            resolve();
          }
        });
      }
    });
  }

  getConnectionState() {
    return this.whepConnection?.connectionState || "not-connected";
  }

  getICEConnectionState() {
    return this.whepConnection?.iceConnectionState || "not-connected";
  }

  isConnected() {
    return this.isWhepConnected;
  }

  async checkStatus() {
    const response = await fetch("/webrtc/status");

    if (!response.ok) {
      throw new Error(await response.text());
    }

    return await response.json();
  }

  startStatusPolling(interval = 10000) {
    this.stopStatusPolling();
    this.webrtcStatusInterval = setInterval(() => {
      this.checkStatus().then((status) => {
        // Auto-disconnect if server stops broadcasting
        if (this.isWhepConnected && !status.broadcasting) {
          console.log("Server stopped broadcasting, disconnecting...");
          this.disconnect();
        }
        this.onStatusUpdate?.(status);
      }).catch((error) => {
        console.error("WebRTC status check error:", error);
      });
    }, interval);
  }

  stopStatusPolling() {
    if (this.webrtcStatusInterval) {
      clearInterval(this.webrtcStatusInterval);
      this.webrtcStatusInterval = null;
    }
  }

  // Event handler setters
  setOnTrackReceived(handler) {
    this.onTrackReceived = handler;
  }

  setOnConnectionStateChange(handler) {
    this.onConnectionStateChange = handler;
  }

  setOnICEConnectionStateChange(handler) {
    this.onICEConnectionStateChange = handler;
  }

  setOnStatusUpdate(handler) {
    this.onStatusUpdate = handler;
  }
}
