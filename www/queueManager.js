export default class QueueManager {
  constructor() {
    this.queue = [];
    this.status = {};
  }

  validateTimestamp(timestamp) {
    if (!timestamp) return true; // Empty is valid

    const regex = /^([0-1]?[0-9]|2[0-3]):[0-5][0-9]:[0-5][0-9]$/;
    return regex.test(timestamp);
  }

  async enqueueFile(fileData) {
    const response = await fetch("/enqueue", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(fileData),
    });

    if (!response.ok) {
      throw new Error(await response.text());
    }

    return await response.json();
  }

  async dequeue(id) {
    const response = await fetch(`/dequeue/${id}`, { method: "DELETE" });

    if (!response.ok) {
      throw new Error(await response.text());
    }

    return await response.text();
  }

  async getQueue() {
    const response = await fetch("/queue");

    if (!response.ok) {
      throw new Error(await response.text());
    }

    const data = await response.json();
    this.queue = data.queue || [];
    this.status = data.status || {};

    return data;
  }

  getQueueItems() {
    return this.queue;
  }

  getStatus() {
    return this.status;
  }

  isRunning() {
    return this.status.running || false;
  }

  isActivelyStreaming() {
    return this.status.activelyStreaming || false;
  }

  getCurrentlyPlaying() {
    return this.status.playing || null;
  }

  getError() {
    return this.status.error || null;
  }

  buildEnqueueData(fileName, overlaySettings, startTimestamp, subtitleFile) {
    const data = {
      file: fileName,
      overlay: overlaySettings,
    };

    if (startTimestamp) {
      data.startTimestamp = startTimestamp;
    }

    if (subtitleFile) {
      data.subtitleFile = subtitleFile;
    }

    return data;
  }
}
