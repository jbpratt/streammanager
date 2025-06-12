export default class FileUI {
  constructor(fileManager, statusManager) {
    this.fileManager = fileManager;
    this.statusManager = statusManager;
  }

  switchToLocalFiles() {
    this.fileManager.setFileMode("local");
    this.updateFileModeUI();
    this.showLocalFileSection();
  }

  switchToServerFiles() {
    this.fileManager.setFileMode("server");
    this.updateFileModeUI();
    this.showServerFileSection();
    return this.loadServerFiles();
  }

  updateFileModeUI() {
    const localBtn = document.getElementById("localFilesBtn");
    const serverBtn = document.getElementById("serverFilesBtn");

    if (this.fileManager.getFileMode() === "local") {
      localBtn.className =
        "flex-1 px-4 py-2 text-sm font-medium text-primary-700 bg-white dark:bg-gray-600 dark:text-primary-300 rounded-md shadow-sm";
      serverBtn.className =
        "flex-1 px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white rounded-md";
    } else {
      localBtn.className =
        "flex-1 px-4 py-2 text-sm font-medium text-gray-700 dark:text-gray-300 hover:text-gray-900 dark:hover:text-white rounded-md";
      serverBtn.className =
        "flex-1 px-4 py-2 text-sm font-medium text-primary-700 bg-white dark:bg-gray-600 dark:text-primary-300 rounded-md shadow-sm";
    }
  }

  showLocalFileSection() {
    document.getElementById("localFileSection").classList.remove("hidden");
    document.getElementById("serverFileSection").classList.add("hidden");
  }

  showServerFileSection() {
    document.getElementById("localFileSection").classList.add("hidden");
    document.getElementById("serverFileSection").classList.remove("hidden");
  }

  async loadServerFiles() {
    const fileBrowser = document.getElementById("fileBrowser");
    fileBrowser.innerHTML =
      '<div class="text-center text-gray-500 py-4">Loading...</div>';

    try {
      const data = await this.fileManager.loadServerFiles();
      this.displayServerFiles(data.files, data.path);
      document.getElementById("currentPath").textContent = `📂 ${
        data.path === "." ? "/" : data.path
      }`;
      return { success: true, data };
    } catch (error) {
      fileBrowser.innerHTML =
        `<div class="text-center text-red-500 py-4">Error: ${error.message}</div>`;
      return { success: false, error: error.message };
    }
  }

  displayServerFiles(files, currentPath) {
    const fileBrowser = document.getElementById("fileBrowser");

    if (files.length === 0) {
      fileBrowser.innerHTML =
        '<div class="text-center text-gray-500 py-4">No video files found</div>';
      return;
    }

    let html = "";

    // Add parent directory link if not at root
    const parentPath = this.fileManager.buildParentPath(currentPath);
    if (parentPath) {
      html += `
                <div class="flex items-center p-2 hover:bg-gray-100 cursor-pointer border-b file-entry" data-path="${parentPath}" data-type="parent">
                    <span class="mr-2">📁</span>
                    <span class="text-blue-600">..</span>
                </div>
            `;
    }

    // Sort and display files
    const sortedFiles = this.fileManager.sortFiles(files);
    sortedFiles.forEach((file) => {
      const icon = file.isDir ? "📁" : "🎬";
      const sizeText = file.isDir
        ? ""
        : ` (${this.fileManager.formatFileSize(file.size)})`;
      const pathClass = file.isDir
        ? "text-primary-600 dark:text-primary-400"
        : "text-gray-700 dark:text-gray-300";

      html += `
                <div class="flex items-center justify-between p-3 hover:bg-gray-50 dark:hover:bg-gray-600 cursor-pointer border-b border-gray-200 dark:border-gray-600 file-entry" data-path="${file.path}" data-type="${
        file.isDir ? "dir" : "file"
      }">
                    <div class="flex items-center flex-1">
                        <span class="mr-3 text-lg">${icon}</span>
                        <div class="flex-1">
                            <span class="${pathClass} font-medium">${file.name}</span>
                            <span class="text-xs text-gray-500 dark:text-gray-400 ml-2">${sizeText}</span>
                        </div>
                    </div>
                    ${
        !file.isDir
          ? '<div class="text-xs text-gray-400 dark:text-gray-500">' +
            new Date(file.modTime).toLocaleDateString() + "</div>"
          : ""
      }
                </div>
            `;
    });

    fileBrowser.innerHTML = html;
    this.attachFileEntryListeners();
  }

  attachFileEntryListeners() {
    const fileBrowser = document.getElementById("fileBrowser");
    fileBrowser.querySelectorAll(".file-entry").forEach((entry) => {
      entry.addEventListener("click", (e) => {
        const path = e.currentTarget.getAttribute("data-path");
        const type = e.currentTarget.getAttribute("data-type");

        if (type === "dir" || type === "parent") {
          this.fileManager.setCurrentPath(path);
          this.loadServerFiles();
        } else if (type === "file") {
          this.selectServerFile(path);
        }
      });
    });
  }

  selectServerFile(filePath) {
    // Clear previous selection
    document.querySelectorAll(".file-entry").forEach((entry) => {
      entry.classList.remove("bg-primary-50", "dark:bg-primary-900/20");
    });

    // Highlight selected file
    const selectedEntry = document.querySelector(
      `[data-path="${filePath}"][data-type="file"]`,
    );
    if (selectedEntry) {
      selectedEntry.classList.add("bg-primary-50", "dark:bg-primary-900/20");
    }

    this.fileManager.setSelectedFile(filePath);
    this.updateServerFileSelection();
  }

  updateServerFileSelection() {
    const enqueueBtn = document.getElementById("enqueueServerBtn");
    const selectedFile = this.fileManager.getSelectedFile();

    if (selectedFile) {
      enqueueBtn.disabled = false;
      enqueueBtn.textContent = `Add: ${selectedFile.split("/").pop()}`;
    } else {
      enqueueBtn.disabled = true;
      enqueueBtn.textContent = "Add Selected File to Queue";
    }
  }

  toggleConfigSettings() {
    const content = document.getElementById("configContent");
    const arrow = document.getElementById("configArrow");

    if (content.style.display === "none") {
      content.style.display = "block";
      arrow.textContent = "▼";
    } else {
      content.style.display = "none";
      arrow.textContent = "▶";
    }
  }

  toggleFileModeSettings() {
    const content = document.getElementById("fileModeContent");
    const arrow = document.getElementById("fileModeArrow");

    if (content.style.display === "none") {
      content.style.display = "block";
      arrow.textContent = "▼";
    } else {
      content.style.display = "none";
      arrow.textContent = "▶";
    }
  }
}
