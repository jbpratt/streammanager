class SettingsManager {
  constructor() {
    this.storageKey = "streammanager_settings";
    this.settingIds = [
      // Stream Configuration
      "destInput",
      "username",
      "password",
      "maxBitrate",
      "encoder",
      "preset",
      "keyframeInterval",
      "ffmpegLogLevel",
      "appLogLevel",
      // Visual Overlays
      "showFilename",
      "overlayPosition",
      "fontSize",
    ];
  }

  // Save all settings to localStorage
  saveSettings() {
    const settings = {};

    this.settingIds.forEach((id) => {
      const element = document.getElementById(id);
      if (element) {
        if (element.type === "checkbox") {
          settings[id] = element.checked;
        } else {
          settings[id] = element.value;
        }
      }
    });

    try {
      localStorage.setItem(this.storageKey, JSON.stringify(settings));
      console.log("Settings saved to localStorage");
    } catch (error) {
      console.error("Failed to save settings:", error);
    }
  }

  // Load settings from localStorage
  loadSettings() {
    try {
      const savedSettings = localStorage.getItem(this.storageKey);
      if (!savedSettings) {
        console.log("No saved settings found");
        return;
      }

      const settings = JSON.parse(savedSettings);
      console.log("Loading saved settings:", settings);

      this.settingIds.forEach((id) => {
        const element = document.getElementById(id);
        if (element && settings[id] !== undefined) {
          if (element.type === "checkbox") {
            element.checked = settings[id];
            // Trigger change event for checkbox to handle overlay options visibility
            element.dispatchEvent(new Event("change"));
          } else {
            element.value = settings[id];
          }
        }
      });

      console.log("Settings loaded from localStorage");
    } catch (error) {
      console.error("Failed to load settings:", error);
    }
  }

  // Clear all saved settings
  clearSettings() {
    try {
      localStorage.removeItem(this.storageKey);
      console.log("Settings cleared from localStorage");
    } catch (error) {
      console.error("Failed to clear settings:", error);
    }
  }

  // Set up event listeners to automatically save settings when they change
  setupAutoSave() {
    this.settingIds.forEach((id) => {
      const element = document.getElementById(id);
      if (element) {
        const eventType = element.type === "checkbox" ? "change" : "input";
        element.addEventListener(eventType, () => {
          // Debounce the save to avoid excessive localStorage writes
          clearTimeout(this.saveTimeout);
          this.saveTimeout = setTimeout(() => {
            this.saveSettings();
          }, 500);
        });
      }
    });
  }

  // Initialize the settings manager
  init() {
    // Load settings when the page loads
    this.loadSettings();
    // Set up auto-save for future changes
    this.setupAutoSave();
  }
}

export default SettingsManager;
