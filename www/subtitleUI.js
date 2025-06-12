export default class SubtitleUI {
  constructor(subtitleManager, fileManager, statusManager) {
    this.subtitleManager = subtitleManager;
    this.fileManager = fileManager;
    this.statusManager = statusManager;
  }

  async showSubtitleBrowser() {
    const modal = document.getElementById("subtitleBrowserModal");
    modal.classList.remove("hidden");

    try {
      const result = await this.subtitleManager.loadSubtitleFiles();
      if (result.success) {
        this.displaySubtitleFiles(result.files, result.path);
        document.getElementById("subtitleCurrentPath").textContent = `📂 ${
          result.path === "." ? "/" : result.path
        }`;
      } else {
        this.showSubtitleError(result.error);
      }
    } catch (error) {
      this.showSubtitleError(error.message);
    }

    this.updateSubtitleSelection();
  }

  hideSubtitleBrowser() {
    this.subtitleManager.hideSubtitleBrowser();
  }

  showSubtitleError(error) {
    const fileBrowser = document.getElementById("subtitleFileBrowser");
    fileBrowser.innerHTML =
      `<div class="text-center text-red-500 py-4">Error: ${error}</div>`;
  }

  async refreshSubtitleFiles() {
    const fileBrowser = document.getElementById("subtitleFileBrowser");
    fileBrowser.innerHTML =
      '<div class="text-center text-gray-500 py-4">Loading...</div>';

    try {
      const result = await this.subtitleManager.loadSubtitleFiles();
      if (result.success) {
        this.displaySubtitleFiles(result.files, result.path);
        document.getElementById("subtitleCurrentPath").textContent = `📂 ${
          result.path === "." ? "/" : result.path
        }`;
      } else {
        this.showSubtitleError(result.error);
      }
    } catch (error) {
      this.showSubtitleError(error.message);
    }
  }

  displaySubtitleFiles(files, currentPath) {
    const fileBrowser = document.getElementById("subtitleFileBrowser");

    if (files.length === 0) {
      fileBrowser.innerHTML =
        '<div class="text-center text-gray-500 py-4">No subtitle files found</div>';
      return;
    }

    let html = "";

    // Add parent directory link if not at root
    const parentPath = this.subtitleManager.buildParentPath(currentPath);
    if (parentPath) {
      html += `
                <div class="flex items-center p-2 hover:bg-gray-100 cursor-pointer border-b subtitle-entry" data-path="${parentPath}" data-type="parent">
                    <span class="mr-2">📁</span>
                    <span class="text-blue-600">..</span>
                </div>
            `;
    }

    // Sort and display files
    const sortedFiles = this.subtitleManager.sortFiles(files);
    sortedFiles.forEach((file) => {
      const icon = file.isDir ? "📁" : "📄";
      const sizeText = file.isDir
        ? ""
        : ` (${this.fileManager.formatFileSize(file.size)})`;
      const pathClass = file.isDir
        ? "text-primary-600 dark:text-primary-400"
        : "text-gray-700 dark:text-gray-300";

      html += `
                <div class="flex items-center justify-between p-3 hover:bg-gray-50 dark:hover:bg-gray-600 cursor-pointer border-b border-gray-200 dark:border-gray-600 subtitle-entry" data-path="${file.path}" data-type="${
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
    this.attachSubtitleEntryListeners();
  }

  attachSubtitleEntryListeners() {
    const fileBrowser = document.getElementById("subtitleFileBrowser");
    fileBrowser.querySelectorAll(".subtitle-entry").forEach((entry) => {
      entry.addEventListener("click", (e) => {
        const path = e.currentTarget.getAttribute("data-path");
        const type = e.currentTarget.getAttribute("data-type");

        if (type === "dir" || type === "parent") {
          this.subtitleManager.setCurrentPath(path);
          this.refreshSubtitleFiles();
        } else if (type === "file") {
          this.selectSubtitleFile(path);
        }
      });
    });
  }

  selectSubtitleFile(filePath) {
    // Clear previous selection
    document.querySelectorAll(".subtitle-entry").forEach((entry) => {
      entry.classList.remove("bg-primary-50", "dark:bg-primary-900/20");
    });

    // Highlight selected file
    const selectedEntry = document.querySelector(
      `[data-path="${filePath}"][data-type="file"]`,
    );
    if (selectedEntry) {
      selectedEntry.classList.add("bg-primary-50", "dark:bg-primary-900/20");
    }

    this.subtitleManager.selectSubtitleFile(filePath);
    this.updateSubtitleSelection();
  }

  updateSubtitleSelection() {
    const selectBtn = document.getElementById("selectSubtitleBtn");
    const selectedFile = this.subtitleManager.getSelectedSubtitleFile();

    if (selectedFile) {
      selectBtn.disabled = false;
      selectBtn.textContent = `Select: ${selectedFile.split("/").pop()}`;
    } else {
      selectBtn.disabled = true;
      selectBtn.textContent = "Select File";
    }
  }

  confirmSubtitleSelection() {
    return this.subtitleManager.confirmSubtitleSelection();
  }
}
