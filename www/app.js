import ThemeManager from "./themeManager.js";
import OverlayManager from "./overlaysManager.js";
import FileManager from "./fileManager.js";
import QueueManager from "./queueManager.js";
import StreamController from "./streamController.js";
import WebRTCManager from "./webrtcManager.js";
import SubtitleManager from "./subtitleManager.js";
import ProgressManager from "./progressManager.js";
import StatusManager from "./statusManager.js";
import FileUI from "./fileUI.js";
import SubtitleUI from "./subtitleUI.js";
import SettingsManager from "./settingsManager.js";

class StreamManager {
  constructor() {
    // Initialize all managers
    this.themeManager = new ThemeManager();
    this.overlaysManager = new OverlayManager();
    this.fileManager = new FileManager();
    this.queueManager = new QueueManager();
    this.streamController = new StreamController();
    this.webrtcManager = new WebRTCManager();
    this.subtitleManager = new SubtitleManager(this.fileManager);
    this.progressManager = new ProgressManager();
    this.statusManager = new StatusManager();
    this.settingsManager = new SettingsManager();

    // UI managers
    this.fileUI = new FileUI(this.fileManager, this.statusManager);
    this.subtitleUI = new SubtitleUI(
      this.subtitleManager,
      this.fileManager,
      this.statusManager,
    );

    this.setupManagerCallbacks();
    this.initEventListeners();
    this.settingsManager.init();
  }

  setupManagerCallbacks() {
    // Set up progress manager callbacks
    this.progressManager.setOnProgressUpdate((progress) => {
      this.statusManager.showProgress(progress);
    });

    this.progressManager.setOnNoProgress(() => {
      if (!this.streamController.getRunning()) {
        this.statusManager.hideProgress();
      }
    });

    // Set up WebRTC manager callbacks
    this.webrtcManager.setOnTrackReceived((event) => {
      const video = document.getElementById("whepVideo");
      if (video.srcObject !== event.streams[0]) {
        video.srcObject = event.streams[0];
        document.getElementById("noStreamMessage").style.display = "none";
      }
    });

    this.webrtcManager.setOnConnectionStateChange((state) => {
      this.statusManager.updateConnectionStats(state, null);

      if (state === "connected") {
        this.statusManager.updateWebRTCStatus("Connected", "text-green-600");
        this.statusManager.showWebRTCButtons(false, true);
        document.getElementById("webrtcStats").classList.remove("hidden");
      } else if (state === "failed" || state === "disconnected") {
        this.statusManager.updateWebRTCStatus("Not connected", "text-gray-600");
        this.statusManager.showWebRTCButtons(true, false);
        document.getElementById("webrtcStats").classList.add("hidden");

        const video = document.getElementById("whepVideo");
        video.srcObject = null;
        document.getElementById("noStreamMessage").style.display = "flex";
      }
    });

    this.webrtcManager.setOnICEConnectionStateChange((state) => {
      this.statusManager.updateConnectionStats(null, state);
    });
  }

  validateTimestamp(timestamp) {
    return this.queueManager.validateTimestamp(timestamp);
  }

  initEventListeners() {
    document.getElementById("themeToggle").addEventListener(
      "click",
      () => this.themeManager.toggleTheme(),
    );

    document.getElementById("showFilename").addEventListener(
      "change",
      () => this.overlaysManager.toggleOverlayOptions(),
    );
    document.getElementById("overlayToggle").addEventListener(
      "click",
      () => this.overlaysManager.toggleOverlaySettings(),
    );

    // Stream controls
    document.getElementById("toggleBtn").addEventListener(
      "click",
      () => this.toggle(),
    );
    document.getElementById("enqueueLocalBtn").addEventListener(
      "click",
      () => this.enqueueLocal(),
    );
    document.getElementById("enqueueServerBtn").addEventListener(
      "click",
      () => this.enqueueServer(),
    );
    document.getElementById("skipBtn").addEventListener(
      "click",
      () => this.skip(),
    );

    // Collapsible sections
    document.getElementById("configToggle").addEventListener(
      "click",
      () => this.fileUI.toggleConfigSettings(),
    );
    document.getElementById("fileModeToggle").addEventListener(
      "click",
      () => this.fileUI.toggleFileModeSettings(),
    );

    // File management
    document.getElementById("localFilesBtn").addEventListener(
      "click",
      () => this.fileUI.switchToLocalFiles(),
    );
    document.getElementById("serverFilesBtn").addEventListener(
      "click",
      () => this.fileUI.switchToServerFiles(),
    );
    document.getElementById("refreshBtn").addEventListener(
      "click",
      () => this.fileUI.loadServerFiles(),
    );

    // Subtitle browser event listeners
    document.getElementById("browseSubtitleBtn").addEventListener(
      "click",
      () => this.handleSubtitleBrowse("local"),
    );
    document.getElementById("browseSubtitleServerBtn").addEventListener(
      "click",
      () => this.handleSubtitleBrowse("server"),
    );
    document.getElementById("closeSubtitleBrowser").addEventListener(
      "click",
      () => this.subtitleUI.hideSubtitleBrowser(),
    );
    document.getElementById("cancelSubtitleBtn").addEventListener(
      "click",
      () => this.subtitleUI.hideSubtitleBrowser(),
    );
    document.getElementById("selectSubtitleBtn").addEventListener(
      "click",
      () => this.subtitleUI.confirmSubtitleSelection(),
    );
    document.getElementById("refreshSubtitleBtn").addEventListener(
      "click",
      () => this.subtitleUI.refreshSubtitleFiles(),
    );

    // WebRTC event listeners
    document.getElementById("whepConnect").addEventListener(
      "click",
      () => this.connectWHEP(),
    );
    document.getElementById("whepDisconnect").addEventListener(
      "click",
      () => this.disconnectWHEP(),
    );

    // Log level event listeners
    document.getElementById("appLogLevel").addEventListener(
      "change",
      () => this.setAppLogLevel(),
    );

    // Auto-refresh queue on page load
    this.getQueue();

    // Load initial application log level
    this.loadAppLogLevel();

    // Auto-refresh queue every 5 seconds
    setInterval(() => this.getQueue(), 5000);

    // Start progress polling
    this.progressManager.start();

    // Start WebRTC status polling
    this.webrtcManager.startStatusPolling();
  }

  toggle() {
    if (this.streamController.getRunning()) {
      this.stop();
    } else {
      this.start();
    }
  }

  updateToggleButton() {
    this.statusManager.updateToggleButton(this.streamController.getRunning());
  }

  async start() {
    const dest = document.getElementById("destInput").value.trim();

    if (!dest) {
      this.statusManager.showStatus("Please enter a destination URL", false);
      return;
    }

    const config = this.streamController.createStartConfig(
      dest,
      document.getElementById("maxBitrate").value,
      document.getElementById("username").value.trim(),
      document.getElementById("password").value.trim(),
      document.getElementById("encoder").value,
      document.getElementById("preset").value,
      document.getElementById("keyframeInterval").value,
      document.getElementById("ffmpegLogLevel").value,
    );

    try {
      const result = await this.streamController.start(config);
      if (result.success) {
        this.updateToggleButton();
        this.getQueue(); // Refresh queue immediately
        this.progressManager.start(); // Start progress updates
      }

      this.statusManager.showStatus(result.message, result.success);
    } catch (error) {
      this.statusManager.showStatus("Error: " + error.message, false);
    }
  }

  async enqueueLocal() {
    const fileInput = document.getElementById("fileInput");
    if (!fileInput.files.length) {
      this.statusManager.showStatus("Please select a file first", false);
      return;
    }

    const fileName = fileInput.files[0].name;

    // Collect overlay settings
    const overlaySettings = {
      showFilename: document.getElementById("showFilename").checked,
      position: document.getElementById("overlayPosition").value,
      fontSize: parseInt(document.getElementById("fontSize").value),
    };

    // Get start timestamp and subtitle file
    const startTimestamp = document.getElementById("startTimestamp").value
      .trim();
    const subtitleFile = document.getElementById("subtitleFile").value.trim();

    // Validate timestamp format
    if (startTimestamp && !this.validateTimestamp(startTimestamp)) {
      this.statusManager.showStatus(
        "Start timestamp must be in HH:MM:SS format (e.g., 01:30:45)",
        false,
      );
      return;
    }

    try {
      const fileData = this.queueManager.buildEnqueueData(
        fileName,
        overlaySettings,
        startTimestamp,
        subtitleFile,
      );
      const data = await this.queueManager.enqueueFile(fileData);

      this.statusManager.showStatus(
        `File "${data.file}" enqueued with ID: ${data.id}`,
        true,
      );
      fileInput.value = ""; // Clear the file input
      this.getQueue(); // Refresh queue immediately
    } catch (error) {
      this.statusManager.showStatus("Error: " + error.message, false);
    }
  }

  async enqueueServer() {
    const selectedFile = this.fileManager.getSelectedFile();
    if (!selectedFile) {
      this.statusManager.showStatus("Please select a server file first", false);
      return;
    }

    // Collect overlay settings
    const overlaySettings = {
      showFilename: document.getElementById("showFilename").checked,
      position: document.getElementById("overlayPosition").value,
      fontSize: parseInt(document.getElementById("fontSize").value),
    };

    // Get start timestamp and subtitle file
    const startTimestamp = document.getElementById("startTimestampServer").value
      .trim();
    const subtitleFile = document.getElementById("subtitleFileServer").value
      .trim();

    // Validate timestamp format
    if (startTimestamp && !this.validateTimestamp(startTimestamp)) {
      this.statusManager.showStatus(
        "Start timestamp must be in HH:MM:SS format (e.g., 01:30:45)",
        false,
      );
      return;
    }

    try {
      const fileData = this.queueManager.buildEnqueueData(
        selectedFile,
        overlaySettings,
        startTimestamp,
        subtitleFile,
      );
      const data = await this.queueManager.enqueueFile(fileData);

      this.statusManager.showStatus(
        `Server file "${data.file}" enqueued with ID: ${data.id}`,
        true,
      );
      this.fileManager.clearSelectedFile();
      this.fileUI.updateServerFileSelection();
      this.getQueue(); // Refresh queue immediately
    } catch (error) {
      this.statusManager.showStatus("Error: " + error.message, false);
    }
  }

  async skip() {
    try {
      const result = await this.streamController.skip();
      this.statusManager.showStatus(result.message, result.success);
    } catch (error) {
      this.statusManager.showStatus("Error: " + error.message, false);
    }
  }

  async stop() {
    try {
      const result = await this.streamController.stop();

      if (result.success) {
        this.updateToggleButton();
        this.progressManager.stop(); // Stop progress updates
        this.statusManager.hideProgress(); // Hide progress display
        // Wait a bit before polling to allow server to update
        setTimeout(() => this.getQueue(), 1000);
      }

      this.statusManager.showStatus(result.message, result.success);
    } catch (error) {
      this.statusManager.showStatus("Error: " + error.message, false);
    }
  }

  async getQueue() {
    try {
      const data = await this.queueManager.getQueue();

      // Sync button state with server status
      this.streamController.setRunning(this.queueManager.isRunning());
      this.updateToggleButton();
      this.showQueue(data);
      this.statusManager.showError(this.queueManager.getError());

      // Show/hide progress based on streaming status
      if (
        this.queueManager.isRunning() && this.queueManager.isActivelyStreaming()
      ) {
        if (!this.progressManager.isRunning()) {
          this.progressManager.start();
        }
      } else {
        this.progressManager.stop();
        this.statusManager.hideProgress();
      }
    } catch (error) {
      this.statusManager.showStatus("Error: " + error.message, false);
    }
  }

  async dequeue(id) {
    try {
      const message = await this.queueManager.dequeue(id);
      this.statusManager.showStatus(message, true);
      this.getQueue(); // Refresh the queue after successful removal
    } catch (error) {
      this.statusManager.showStatus("Error: " + error.message, false);
    }
  }

  showQueue(data) {
    const queue = this.statusManager.showQueue(data);

    // Add event listeners to remove buttons
    queue.forEach((btn) => {
      btn.addEventListener("click", (e) => {
        const id = e.target.getAttribute("data-id");
        this.dequeue(id);
      });
    });
  }

  // WebRTC Methods
  async connectWHEP() {
    this.statusManager.updateWebRTCStatus("Connecting...", "text-yellow-600");

    const result = await this.webrtcManager.connect();

    if (!result.success) {
      if (result.status === "no-broadcast") {
        this.statusManager.updateWebRTCStatus(
          "No active broadcast available",
          "text-red-600",
        );
      } else {
        this.statusManager.updateWebRTCStatus(
          "Connection failed",
          "text-red-600",
        );
      }
      this.statusManager.showStatus(result.message, false);
    } else {
      this.statusManager.updateWebRTCStatus(
        "Connecting (ICE)...",
        "text-yellow-600",
      );
    }
  }

  disconnectWHEP() {
    const result = this.webrtcManager.disconnect();

    if (result.success) {
      const video = document.getElementById("whepVideo");
      video.srcObject = null;
      document.getElementById("noStreamMessage").style.display = "flex";
      document.getElementById("webrtcStats").classList.add("hidden");

      this.statusManager.updateWebRTCStatus("Not connected", "text-gray-600");
      this.statusManager.showWebRTCButtons(true, false);
    }
  }

  // Subtitle browser methods
  handleSubtitleBrowse(mode) {
    this.subtitleManager.handleSubtitleBrowse(mode);
  }

  async loadAppLogLevel() {
    try {
      const data = await this.streamController.getLogLevel();
      const appLogLevelSelect = document.getElementById("appLogLevel");
      appLogLevelSelect.value = data.level;
    } catch (error) {
      console.error("Error loading application log level:", error);
    }
  }

  async setAppLogLevel() {
    const appLogLevel = document.getElementById("appLogLevel").value;

    try {
      const data = await this.streamController.setLogLevel(appLogLevel);
      this.statusManager.showStatus(
        `Application log level changed from ${data.old_level} to ${data.level}`,
        true,
      );
    } catch (error) {
      this.statusManager.showStatus(
        "Error setting log level: " + error.message,
        false,
      );
    }
  }
}

document.addEventListener("DOMContentLoaded", () => {
  new StreamManager();
});
