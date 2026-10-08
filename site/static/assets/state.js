// Playground state in the URL fragment: "#z=" followed by the state as JSON, compressed with
// deflate-raw (CompressionStream) and encoded as base64url; "#j=" without compression where
// CompressionStream is unavailable. "#example=<name>" opens an example.

function toBase64URL(bytes) {
  let s = "";
  for (let i = 0; i < bytes.length; i += 0x8000) s += String.fromCharCode(...bytes.subarray(i, i + 0x8000));
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function fromBase64URL(s) {
  const b = atob(s.replace(/-/g, "+").replace(/_/g, "/"));
  const bytes = new Uint8Array(b.length);
  for (let i = 0; i < b.length; i++) bytes[i] = b.charCodeAt(i);
  return bytes;
}

async function pipe(bytes, stream) {
  const out = new Blob([bytes]).stream().pipeThrough(stream);
  return new Uint8Array(await new Response(out).arrayBuffer());
}

// encodeState returns the URL fragment (without "#") for a state object.
export async function encodeState(state) {
  const json = new TextEncoder().encode(JSON.stringify(state));
  if (typeof CompressionStream === "function") {
    try {
      return "z=" + toBase64URL(await pipe(json, new CompressionStream("deflate-raw")));
    } catch {
      // Fall through to the uncompressed form.
    }
  }
  return "j=" + toBase64URL(json);
}

// decodeState parses a URL fragment (with or without "#"). It returns {example} for an example link,
// the state object, or null.
export async function decodeState(hash) {
  const h = hash.replace(/^#/, "");
  try {
    if (h.startsWith("example=")) return { example: decodeURIComponent(h.slice(8)) };
    if (h.startsWith("z=")) {
      const bytes = await pipe(fromBase64URL(h.slice(2)), new DecompressionStream("deflate-raw"));
      return JSON.parse(new TextDecoder().decode(bytes));
    }
    if (h.startsWith("j=")) return JSON.parse(new TextDecoder().decode(fromBase64URL(h.slice(2))));
  } catch (err) {
    console.warn("pego: cannot read the state in the URL:", err);
  }
  return null;
}
