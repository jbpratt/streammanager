export default class SubtitleManager {
  constructor(fileManager) {
    this.fileManager = fileManager;
    this.subtitleCurrentPath = ".";
    this.selectedSubtitleFile = null;
    this.subtitleTargetInput = null;
    this.selectedLocalSubtitleFile = null;
  }

  handleSubtitleBrowse(mode) {
    if (mode === "local") {
      this.browseLocalSubtitles();
    } else {
      this.browseServerSubtitles("subtitleFileServer");
    }
  }

  browseLocalSubtitles() {
    const subtitleFileInput = document.getElementById("subtitleFileInput");
    subtitleFileInput.onchange = (e) => {
      if (e.target.files.length > 0) {
        const fileName = e.target.files[0].name;
        document.getElementById("subtitleFile").value = fileName;
        this.selectedLocalSubtitleFile = e.target.files[0];
      }
    };
    subtitleFileInput.click();
  }

  browseServerSubtitles(targetInputId) {
    this.subtitleTargetInput = targetInputId;
    this.subtitleCurrentPath = ".";
    this.selectedSubtitleFile = null;
    this.showSubtitleBrowser();
  }

  showSubtitleBrowser() {
    const modal = document.getElementById("subtitleBrowserModal");
    modal.classList.remove("hidden");
    return this.loadSubtitleFiles();
  }

  hideSubtitleBrowser() {
    const modal = document.getElementById("subtitleBrowserModal");
    modal.classList.add("hidden");
    this.subtitleTargetInput = null;
    this.selectedSubtitleFile = null;
  }

  async loadSubtitleFiles(path = null) {
    if (path !== null) {
      this.subtitleCurrentPath = path;
    }

    try {
      const url = `/files${
        this.subtitleCurrentPath !== "."
          ? "?path=" + encodeURIComponent(this.subtitleCurrentPath)
          : ""
      }`;
      const response = await fetch(url);

      if (!response.ok) {
        throw new Error(await response.text());
      }

      const data = await response.json();
      const subtitleFiles = this.filterSubtitleFiles(data.files);

      return {
        success: true,
        files: subtitleFiles,
        path: data.path,
      };
    } catch (error) {
      return {
        success: false,
        error: error.message,
      };
    }
  }

  filterSubtitleFiles(files) {
    return files.filter((file) => file.isDir || this.isSubtitleFile(file.name));
  }

  isSubtitleFile(filename) {
    const ext = filename.toLowerCase().split(".").pop();
    const subtitleExtensions = ["srt", "vtt", "ass", "ssa", "sub", "sbv"];
    return subtitleExtensions.includes(ext);
  }

  selectSubtitleFile(filePath) {
    this.selectedSubtitleFile = filePath;
    return this.selectedSubtitleFile;
  }

  confirmSubtitleSelection() {
    if (this.selectedSubtitleFile && this.subtitleTargetInput) {
      const input = document.getElementById(this.subtitleTargetInput);
      if (input) {
        input.value = this.selectedSubtitleFile;
      }
    }
    this.hideSubtitleBrowser();
    return this.selectedSubtitleFile;
  }

  getSelectedSubtitleFile() {
    return this.selectedSubtitleFile;
  }

  getSelectedLocalSubtitleFile() {
    return this.selectedLocalSubtitleFile;
  }

  getCurrentPath() {
    return this.subtitleCurrentPath;
  }

  setCurrentPath(path) {
    this.subtitleCurrentPath = path;
  }

  buildParentPath(currentPath) {
    if (currentPath === "." || currentPath === "/") {
      return null;
    }
    return currentPath.split("/").slice(0, -1).join("/") || ".";
  }

  sortFiles(files) {
    return [...files].sort((a, b) => {
      if (a.isDir && !b.isDir) return -1;
      if (!a.isDir && b.isDir) return 1;
      return a.name.localeCompare(b.name);
    });
  }
}
