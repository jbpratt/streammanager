export default class FileManager {
  constructor() {
    this.currentPath = ".";
    this.selectedFile = null;
    this.fileMode = "local"; // 'local' or 'server'
  }

  setFileMode(mode) {
    this.fileMode = mode;
  }

  getFileMode() {
    return this.fileMode;
  }

  setSelectedFile(file) {
    this.selectedFile = file;
  }

  getSelectedFile() {
    return this.selectedFile;
  }

  clearSelectedFile() {
    this.selectedFile = null;
  }

  setCurrentPath(path) {
    this.currentPath = path;
  }

  getCurrentPath() {
    return this.currentPath;
  }

  async loadServerFiles(path = null) {
    if (path !== null) {
      this.currentPath = path;
    }

    const url = `/files${
      this.currentPath !== "."
        ? "?path=" + encodeURIComponent(this.currentPath)
        : ""
    }`;
    const response = await fetch(url);

    if (!response.ok) {
      throw new Error(await response.text());
    }

    return await response.json();
  }

  formatFileSize(bytes) {
    if (bytes === 0) return "0 Bytes";
    const k = 1024;
    const sizes = ["Bytes", "KB", "MB", "GB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + " " + sizes[i];
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

  isVideoFile(filename) {
    const ext = filename.toLowerCase().split(".").pop();
    const videoExtensions = [
      "mp4",
      "avi",
      "mkv",
      "mov",
      "wmv",
      "flv",
      "webm",
      "m4v",
    ];
    return videoExtensions.includes(ext);
  }
}
