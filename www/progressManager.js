export default class ProgressManager {
  constructor() {
    this.progressInterval = null;
    this.lastProgressTime = null;
  }

  start() {
    this.stop(); // Stop any existing interval
    this.progressInterval = setInterval(() => this.fetchProgress(), 2000);
    this.fetchProgress(); // Get initial progress
  }

  stop() {
    if (this.progressInterval) {
      clearInterval(this.progressInterval);
      this.progressInterval = null;
    }
  }

  async fetchProgress() {
    try {
      const response = await fetch("/progress");
      if (response.ok) {
        const data = await response.json();
        if (data.hasProgress && data.progress) {
          this.onProgressUpdate?.(data.progress);
          return { success: true, data: data.progress };
        } else {
          this.onNoProgress?.();
          return { success: true, data: null };
        }
      } else {
        return { success: false, error: await response.text() };
      }
    } catch (error) {
      console.error("Progress polling error:", error);
      return { success: false, error: error.message };
    }
  }

  formatProgress(progress) {
    return {
      frame: progress.frame?.toLocaleString() || "-",
      fps: progress.fps ? progress.fps.toFixed(1) : "-",
      bitrate: progress.bitrate || "-",
      time: progress.out_time || "-",
      speed: progress.speed || "-",
      timestamp: progress.timestamp
        ? new Date(progress.timestamp).toLocaleTimeString()
        : "-",
      percentage: progress.percentage
        ? progress.percentage.toFixed(1) + "%"
        : "-",
      duration: progress.duration
        ? this.formatDuration(progress.duration)
        : "-",
    };
  }

  calculateProgressPercentage(progress) {
    // Use accurate percentage from backend if available
    if (progress.percentage !== undefined && progress.percentage >= 0) {
      return Math.min(Math.max(progress.percentage, 0), 100);
    }
    return 0;
  }

  formatDuration(seconds) {
    const hours = Math.floor(seconds / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    const secs = Math.floor(seconds % 60);

    if (hours > 0) {
      return `${hours}:${minutes.toString().padStart(2, "0")}:${
        secs.toString().padStart(2, "0")
      }`;
    }
    return `${minutes}:${secs.toString().padStart(2, "0")}`;
  }

  isRunning() {
    return this.progressInterval !== null;
  }

  setOnProgressUpdate(handler) {
    this.onProgressUpdate = handler;
  }

  setOnNoProgress(handler) {
    this.onNoProgress = handler;
  }

  getLastProgressTime() {
    return this.lastProgressTime;
  }

  setLastProgressTime(timestamp) {
    this.lastProgressTime = timestamp;
  }
}
