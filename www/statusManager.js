export default class StatusManager {
  constructor() {
    this.statusTimeout = null;
  }

  showStatus(message, isSuccess) {
    const statusDiv = document.getElementById("status");
    statusDiv.textContent = message;
    statusDiv.className = `mb-6 p-4 rounded-lg ${
      isSuccess
        ? "bg-green-50 dark:bg-green-900/20 text-green-800 dark:text-green-200 border border-green-200 dark:border-green-800"
        : "bg-red-50 dark:bg-red-900/20 text-red-800 dark:text-red-200 border border-red-200 dark:border-red-800"
    }`;
    statusDiv.classList.remove("hidden");

    // Clear existing timeout
    if (this.statusTimeout) {
      clearTimeout(this.statusTimeout);
    }

    // Auto-hide after 5 seconds
    this.statusTimeout = setTimeout(() => {
      statusDiv.classList.add("hidden");
    }, 5000);
  }

  hideStatus() {
    const statusDiv = document.getElementById("status");
    statusDiv.classList.add("hidden");

    if (this.statusTimeout) {
      clearTimeout(this.statusTimeout);
      this.statusTimeout = null;
    }
  }

  showError(error) {
    const errorDiv = document.getElementById("errorNotification");

    if (error) {
      const errorTime = new Date(error.time * 1000).toLocaleTimeString();
      errorDiv.innerHTML = `
                <div class="bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 text-red-800 dark:text-red-200 px-4 py-3 rounded-lg">
                    <div class="font-semibold">⚠️ FFmpeg Error (${errorTime})</div>
                    <div class="mt-1 text-sm whitespace-pre-wrap">${error.message}</div>
                </div>
            `;
      errorDiv.classList.remove("hidden");
    } else {
      errorDiv.classList.add("hidden");
    }
  }

  showProgress(progress) {
    const progressDiv = document.getElementById("progress");
    progressDiv.classList.remove("hidden");

    // Update progress data
    document.getElementById("progressFrame").textContent =
      progress.frame?.toLocaleString() || "-";
    document.getElementById("progressFps").textContent = progress.fps
      ? progress.fps.toFixed(1)
      : "-";
    document.getElementById("progressBitrate").textContent = progress.bitrate ||
      "-";
    document.getElementById("progressTime").textContent = progress.out_time ||
      "-";
    document.getElementById("progressSpeed").textContent = progress.speed ||
      "-";

    // Update timestamp
    if (progress.timestamp) {
      const timestamp = new Date(progress.timestamp);
      document.getElementById("progressTimestamp").textContent = timestamp
        .toLocaleTimeString();
    }

    // Calculate and show progress bar
    if (progress.frame && progress.fps) {
      const estimatedTotalFrames = progress.fps * 300; // Assume 5 min video max for demo
      const percentage = Math.min(
        (progress.frame / estimatedTotalFrames) * 100,
        100,
      );

      const progressBar = document.getElementById("progressBar");
      const progressPercentage = document.getElementById("progressPercentage");

      progressBar.style.width = percentage.toFixed(1) + "%";
      progressPercentage.textContent = percentage.toFixed(1) + "%";
    }
  }

  hideProgress() {
    const progressDiv = document.getElementById("progress");
    progressDiv.classList.add("hidden");

    // Reset progress values
    document.getElementById("progressFrame").textContent = "-";
    document.getElementById("progressFps").textContent = "-";
    document.getElementById("progressBitrate").textContent = "-";
    document.getElementById("progressTime").textContent = "-";
    document.getElementById("progressSpeed").textContent = "-";
    document.getElementById("progressTimestamp").textContent = "-";
    document.getElementById("progressBar").style.width = "0%";
    document.getElementById("progressPercentage").textContent = "0%";
  }

  showQueue(data) {
    const queueDiv = document.getElementById("queue");
    const queueItems = data.queue || [];
    const status = data.status || {};

    let statusText = "Stopped";
    let statusColor = "text-gray-600 dark:text-gray-400";
    if (status.running) {
      statusText = status.activelyStreaming ? "Streaming" : "Ready";
      statusColor = status.activelyStreaming
        ? "text-green-600 dark:text-green-400"
        : "text-yellow-600 dark:text-yellow-400";
    }

    let html = `
            <div class="flex items-center justify-between mb-4 p-3 bg-gray-50 dark:bg-gray-700 rounded-lg">
                <div>
                    <span class="text-sm font-medium text-gray-700 dark:text-gray-300">Status:</span>
                    <span class="ml-2 font-semibold ${statusColor}">${statusText}</span>
                </div>
                <span class="text-sm text-gray-500 dark:text-gray-400">${queueItems.length} items</span>
            </div>
        `;

    if (status.playing) {
      html += `
                <div class="mb-4 p-3 bg-blue-50 dark:bg-blue-900/20 border border-blue-200 dark:border-blue-800 rounded-lg">
                    <div class="text-sm font-medium text-blue-800 dark:text-blue-200">Currently Playing:</div>
                    <div class="text-sm text-blue-600 dark:text-blue-300 mt-1">${status.playing.file}</div>
                </div>
            `;
    }

    if (queueItems.length === 0) {
      html += `
                <div class="text-center py-8 text-gray-500 dark:text-gray-400">
                    <div class="text-2xl mb-2">📂</div>
                    <div>No items in queue</div>
                    <div class="text-sm mt-1">Add files to start streaming</div>
                </div>
            `;
    } else {
      queueItems.forEach((item, index) => {
        html += `
                    <div class="flex justify-between items-center p-3 bg-gray-50 dark:bg-gray-700 border border-gray-200 dark:border-gray-600 rounded-lg mb-2">
                        <div class="flex-1">
                            <div class="text-sm font-medium text-gray-900 dark:text-white">${
          index + 1
        }. ${item.file}</div>
                        </div>
                        <button class="remove-btn bg-red-500 hover:bg-red-600 text-white px-3 py-1 rounded-lg text-sm font-medium transition-colors" data-id="${item.id}">
                            Remove
                        </button>
                    </div>
                `;
      });
    }

    queueDiv.innerHTML = html;
    return queueDiv;
  }

  updateToggleButton(isRunning) {
    const toggleBtn = document.getElementById("toggleBtn");
    if (isRunning) {
      toggleBtn.textContent = "Stop Stream";
      toggleBtn.className =
        "w-full bg-red-500 text-white px-4 py-2 rounded hover:bg-red-600";
      toggleBtn.setAttribute("data-state", "running");
    } else {
      toggleBtn.textContent = "Start Stream";
      toggleBtn.className =
        "w-full bg-green-500 text-white px-4 py-2 rounded hover:bg-green-600";
      toggleBtn.setAttribute("data-state", "stopped");
    }
  }

  updateWebRTCStatus(message, className = "text-gray-600") {
    const statusElement = document.getElementById("webrtcStatus");
    statusElement.textContent = message;
    statusElement.className = `text-sm ${className}`;
  }

  showWebRTCButtons(showConnect, showDisconnect) {
    const connectBtn = document.getElementById("whepConnect");
    const disconnectBtn = document.getElementById("whepDisconnect");

    if (showConnect) {
      connectBtn.classList.remove("hidden");
    } else {
      connectBtn.classList.add("hidden");
    }

    if (showDisconnect) {
      disconnectBtn.classList.remove("hidden");
    } else {
      disconnectBtn.classList.add("hidden");
    }
  }

  updateConnectionStats(connectionState, iceState) {
    if (connectionState) {
      document.getElementById("connectionState").textContent = connectionState;
    }
    if (iceState) {
      document.getElementById("iceState").textContent = iceState;
    }
  }
}
