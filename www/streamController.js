export default class StreamController {
  constructor() {
    this.isRunning = false;
  }

  async start(config) {
    const response = await fetch("/start", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(config),
    });

    const text = await response.text();

    if (response.ok) {
      this.isRunning = true;
    }

    return {
      success: response.ok,
      message: text,
      isRunning: this.isRunning,
    };
  }

  async stop() {
    const response = await fetch("/stop", { method: "POST" });
    const text = await response.text();

    if (response.ok) {
      this.isRunning = false;
    }

    return {
      success: response.ok,
      message: text,
      isRunning: this.isRunning,
    };
  }

  async skip() {
    const response = await fetch("/skip", { method: "POST" });
    const text = await response.text();

    return {
      success: response.ok,
      message: text,
    };
  }

  setRunning(running) {
    this.isRunning = running;
  }

  getRunning() {
    return this.isRunning;
  }

  createStartConfig(
    destination,
    maxBitrate,
    username,
    password,
    encoder,
    preset,
    keyframeInterval,
    logLevel,
  ) {
    const config = {
      destination: destination,
      maxBitrate: maxBitrate,
      username: username,
      encoder: encoder,
      preset: preset,
      keyframeInterval: keyframeInterval,
      logLevel: logLevel,
    };

    // Add password field using bracket notation
    config["password"] = password;

    return config;
  }

  buildStartConfig(
    destInput,
    maxBitrate,
    username,
    password,
    encoder,
    preset,
    keyframeInterval,
    logLevel,
  ) {
    return {
      destination: destInput,
      maxBitrate: maxBitrate,
      username: username,
      password: password,
      encoder: encoder,
      preset: preset,
      keyframeInterval: keyframeInterval,
      logLevel: logLevel,
    };
  }

  async getLogLevel() {
    const response = await fetch("/log-level");

    if (!response.ok) {
      throw new Error(await response.text());
    }

    return await response.json();
  }

  async setLogLevel(level) {
    const response = await fetch("/log-level", {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ level: level }),
    });

    if (!response.ok) {
      throw new Error(await response.text());
    }

    return await response.json();
  }
}
