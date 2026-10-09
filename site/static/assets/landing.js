// The live example on the landing page: parses the input with the README's grammar as you type.

import { PegoClient, siteRoot } from "./pego-client.js";
import { encodeState } from "./state.js";

const live = document.getElementById("live");
const input = document.getElementById("live-input");
const output = document.getElementById("live-output");
const status = document.getElementById("live-status");
const open = document.getElementById("live-open");

if (live && input && output) {
  const grammar = live.dataset.grammar;
  const client = new PegoClient(siteRoot() + "playground/");
  let timer = 0;
  let seq = 0;
  let linkVersion = 0;

  const setStatus = (text, cls) => {
    status.textContent = text;
    status.className = "live-status " + (cls || "");
  };

  const run = async () => {
    const n = ++seq;
    try {
      const res = await client.call("parse", { grammar, input: input.value }, { key: "parse" });
      if (n !== seq) return;
      if (res.errors?.length) {
        const e = res.errors[0];
        output.textContent = `${e.line}:${e.col}: ${e.message}`;
        setStatus("syntax error", "error");
      } else if (res.compile && !res.compile.ok) {
        output.textContent = res.compile.diagnostics.map((d) => `${d.line}:${d.col}: ${d.message}`).join("\n");
        setStatus("grammar error", "error");
      } else if (res.error) {
        output.textContent = res.error;
        setStatus("error", "error");
      } else {
        output.textContent = res.sexpr;
        setStatus(`${res.nodes} nodes · ${(res.micros / 1000).toFixed(2)} ms`, "ok");
      }
    } catch (err) {
      if (n !== seq) return;
      output.textContent = String(err.message || err);
      setStatus("unavailable", "error");
    }
  };

  const updateLink = async () => {
    const version = ++linkVersion;
    const text = input.value;
    try {
      const hash = await encodeState({ g: grammar, i: text });
      if (version === linkVersion && input.value === text) open.href = siteRoot() + "playground/#" + hash;
    } catch (err) {
      if (version === linkVersion) console.warn("pego: cannot update the playground link:", err);
    }
  };

  input.addEventListener("input", () => {
    clearTimeout(timer);
    timer = setTimeout(run, 120);
    updateLink();
  });

  // Load the parser (about 2 MB compressed) only when the example is about to be seen.
  const startNow = () => {
    run();
    updateLink();
  };
  if ("IntersectionObserver" in window) {
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) {
        io.disconnect();
        startNow();
      }
    }, { rootMargin: "200px" });
    io.observe(live);
  } else {
    startNow();
  }
}
