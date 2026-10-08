// Applies the saved color theme before the first paint (loaded synchronously in <head>).
try {
  const t = localStorage.getItem("pego-theme");
  if (t === "light" || t === "dark") document.documentElement.dataset.theme = t;
} catch {
  // Storage can be unavailable; the system theme applies.
}
