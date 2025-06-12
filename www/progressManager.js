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
    };
  }

  calculateProgressPercentage(progress) {
    // This is a rough estimation - in a real scenario you'd need total duration
    if (progress.frame && progress.fps) {
      const estimatedTotalFrames = progress.fps * 300; // Assume 5 min video max for demo
      return Math.min((progress.frame / estimatedTotalFrames) * 100, 100);
    }
    return 0;
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
