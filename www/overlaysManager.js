class OverlayManager {
  constructor() {
    this.toggleOverlayOptions();
  }

  toggleOverlayOptions() {
    const checkbox = document.getElementById("showFilename");
    const options = document.getElementById("overlayOptions");
    if (checkbox.checked) {
      options.classList.remove("hidden");
    } else {
      options.classList.add("hidden");
    }
  }

  toggleOverlaySettings() {
    const content = document.getElementById("overlayContent");
    const arrow = document.getElementById("overlayArrow");
    if (content.style.display === "none") {
      content.style.display = "block";
      arrow.textContent = "▼";
    } else {
      content.style.display = "none";
      arrow.textContent = "▶";
    }
  }
}

export default OverlayManager;
