// Renders ```mermaid fences as diagrams. The build adds this script only to pages that contain such
// a fence. The page's Content-Security-Policy allows no remote scripts, so Mermaid is vendored
// (vendor/). Mermaid styles the SVG it builds inline, which the policy's style-src permits for
// this reason; the finished SVG is shown through <img> elements, so nothing of it stays in the
// page. Each diagram is rendered once per color theme; the stylesheet shows the one that matches
// the page. The fence text stays in place when rendering fails or without
// JavaScript.
const MERMAID = new URL("vendor/mermaid-11.12.0.min.js", import.meta.url).href;
const FONT = "ui-sans-serif, system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif";

// loadMermaid loads the vendored build, which defines the global mermaid.
function loadMermaid() {
  return new Promise((resolve, reject) => {
    if (globalThis.mermaid) return resolve(globalThis.mermaid);
    const s = document.createElement("script");
    s.src = MERMAID;
    s.onload = () => resolve(globalThis.mermaid);
    s.onerror = () => reject(new Error("cannot load " + MERMAID));
    document.head.append(s);
  });
}

// svgImage returns an <img> showing the SVG markup.
function svgImage(svg, alt, theme) {
  const img = document.createElement("img");
  img.className = "mermaid-" + theme;
  img.alt = alt;
  img.src = "data:image/svg+xml;charset=utf-8," + encodeURIComponent(svg);
  return img;
}

async function main() {
  const codes = [...document.querySelectorAll("pre > code.language-mermaid")];
  if (codes.length === 0) return;
  const mermaid = await loadMermaid();
  let n = 0;
  for (const theme of ["light", "dark"]) {
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      theme: theme === "dark" ? "dark" : "default",
      themeVariables: { fontFamily: FONT, background: "transparent" },
      flowchart: { htmlLabels: false },
    });
    for (const code of codes) {
      const text = code.textContent;
      try {
        const { svg } = await mermaid.render("mermaid-" + n++, text);
        (code.mermaidImages ??= []).push(svgImage(svg, text.trim(), theme));
      } catch (err) {
        console.error("Mermaid diagram failed:", err);
        code.mermaidFailed = true;
      }
    }
  }
  for (const code of codes) {
    if (code.mermaidFailed || !code.mermaidImages) continue;
    const pre = code.parentElement;
    const figure = document.createElement("figure");
    figure.className = "mermaid";
    figure.append(...code.mermaidImages);
    pre.replaceWith(figure);
  }
}

main().catch((err) => console.error(err));
