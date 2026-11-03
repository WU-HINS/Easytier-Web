// Runs before first paint: applies the stored theme and background so a dark
// user never sees a white flash. Kept as an external file so the Content
// Security Policy can stay script-src 'self' (no inline script).
(function () {
  try {
    var mode = localStorage.getItem('theme') || 'auto';
    var dark = mode === 'dark' || (mode === 'auto' && window.matchMedia('(prefers-color-scheme: dark)').matches);
    if (dark) document.documentElement.classList.add('dark');
    document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
    var bg = localStorage.getItem('background');
    if (bg && /^https?:\/\//.test(bg)) {
      document.documentElement.style.setProperty('--app-bg-image', 'url(' + JSON.stringify(bg) + ')');
      document.documentElement.dataset.image = 'true';
    }
  } catch (e) {}
})();
