class ThemeManager {
  constructor() {
    this.initTheme();
  }

  initTheme() {
    const savedTheme = localStorage.getItem("theme");
    const prefersDark =
      window.matchMedia("(prefers-color-scheme: dark)").matches;
    const theme = savedTheme || (prefersDark ? "dark" : "light");
    this.setTheme(theme);
  }

  setTheme(theme) {
    const html = document.documentElement;
    const themeIcon = document.getElementById("themeIcon");
    if (theme === "dark") {
      html.classList.add("dark");
      themeIcon.textContent = "☀️";
    } else {
      html.classList.remove("dark");
      themeIcon.textContent = "🌙";
    }
    localStorage.setItem("theme", theme);
  }

  toggleTheme() {
    const html = document.documentElement;
    const isDark = html.classList.contains("dark");
    this.setTheme(isDark ? "light" : "dark");
  }
}

export default ThemeManager;
