package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Only the published current pair is inherited. A previous pair never becomes
// current, so successive builds cannot accumulate historical binaries.
type assetManifest struct {
	Version  int              `json:"version"`
	Source   string           `json:"source,omitempty"`
	Current  playgroundAsset  `json:"current"`
	Previous *playgroundAsset `json:"previous,omitempty"`
}

type playgroundAsset struct {
	Wasm          string `json:"wasm"`
	WasmSHA256    string `json:"wasmSHA256"`
	Runtime       string `json:"runtime"`
	RuntimeSHA256 string `json:"runtimeSHA256"`
}

type assetBytes struct {
	asset         playgroundAsset
	wasm, runtime []byte
}

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var legacyWasmPattern = regexp.MustCompile(`data-wasm="(?:\.\./)?playground/(wasm/pego-[0-9a-f]{16}\.wasm)"`)

func deploymentURLs(getenv func(string) string) (previous, deploy string) {
	if getenv("NETLIFY") == "true" && getenv("CONTEXT") == "production" && getenv("NETLIFY_PREVIEW_SERVER") != "true" {
		return getenv("URL"), getenv("DEPLOY_URL")
	}
	return "", ""
}

func siteURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid published site URL %q", raw)
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/"
	return u, nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func describeAssets(wasm, runtime []byte) playgroundAsset {
	w, r := hashBytes(wasm), hashBytes(runtime)
	return playgroundAsset{Wasm: "wasm/pego-" + w[:16] + ".wasm", WasmSHA256: w,
		Runtime: "runtime/wasm_exec-" + r[:16] + ".js", RuntimeSHA256: r}
}

func (a playgroundAsset) validate() error {
	if !digestPattern.MatchString(a.WasmSHA256) || !digestPattern.MatchString(a.RuntimeSHA256) ||
		a.Wasm != "wasm/pego-"+a.WasmSHA256[:16]+".wasm" ||
		a.Runtime != "runtime/wasm_exec-"+a.RuntimeSHA256[:16]+".js" {
		return errors.New("invalid playground asset paths or hashes")
	}
	return nil
}

func fetchAsset(client *http.Client, base *url.URL, path string, limit int64) ([]byte, int, error) {
	u := base.ResolveReference(&url.URL{Path: path})
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, resp.StatusCode, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("fetching %s: HTTP %d", u, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	if int64(len(b)) > limit {
		return nil, resp.StatusCode, fmt.Errorf("%s exceeds %d bytes", u, limit)
	}
	return b, resp.StatusCode, nil
}

func readPreviousAssets(raw string) (*assetBytes, error) {
	if raw == "" {
		return nil, nil
	}
	base, err := siteURL(raw)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	b, status, err := fetchAsset(client, base, "playground/assets.json", 64<<10)
	if err != nil {
		return nil, err
	}
	var a playgroundAsset
	legacy := status == http.StatusNotFound
	if legacy {
		// Bootstrap sites built before manifests existed, without archiving their
		// whole output or requiring a cache from a previous build machine.
		page, code, err := fetchAsset(client, base, "playground/", 1<<20)
		if err != nil {
			return nil, err
		}
		if code == http.StatusNotFound {
			return nil, nil
		} // first deployment
		m := legacyWasmPattern.FindSubmatch(page)
		if m == nil {
			return nil, errors.New("published playground has no recognized WASM reference")
		}
		a.Wasm, a.Runtime = string(m[1]), "wasm_exec.js"
	} else {
		var m assetManifest
		d := json.NewDecoder(bytes.NewReader(b))
		d.DisallowUnknownFields()
		if err := d.Decode(&m); err != nil {
			return nil, fmt.Errorf("published asset manifest: %w", err)
		}
		if err := d.Decode(new(any)); err != io.EOF {
			return nil, errors.New("trailing published manifest data")
		}
		if m.Version != 1 {
			return nil, fmt.Errorf("unsupported asset manifest version %d", m.Version)
		}
		if err := m.Current.validate(); err != nil {
			return nil, err
		}
		a = m.Current
		if m.Source != "" {
			base, err = siteURL(m.Source)
			if err != nil {
				return nil, err
			}
		}
	}
	playground := base.ResolveReference(&url.URL{Path: "playground/"})
	wasm, code, err := fetchAsset(client, playground, a.Wasm, 64<<20)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK || !bytes.HasPrefix(wasm, []byte{'\x00', 'a', 's', 'm', 1, 0, 0, 0}) {
		return nil, errors.New("published WASM is missing or invalid")
	}
	runtime, code, err := fetchAsset(client, playground, a.Runtime, 1<<20)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK || len(runtime) == 0 {
		return nil, errors.New("published Go runtime is missing or empty")
	}
	actual := describeAssets(wasm, runtime)
	if legacy {
		if actual.Wasm != a.Wasm {
			return nil, errors.New("published WASM hash does not match its filename")
		}
	} else if actual != a {
		return nil, errors.New("published playground asset hashes do not match the manifest")
	}
	return &assetBytes{asset: actual, wasm: wasm, runtime: runtime}, nil
}

func publishAssets(dir, name, source string, previous *assetBytes) error {
	wasm, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		return err
	}
	runtime, err := os.ReadFile(filepath.Join(dir, "wasm_exec.js"))
	if err != nil {
		return err
	}
	current := describeAssets(wasm, runtime)
	if current.Wasm != name {
		return errors.New("current WASM hash does not match its filename")
	}
	m := assetManifest{Version: 1, Source: source, Current: current}
	if err := writeFile(filepath.Join(dir, filepath.FromSlash(current.Runtime)), runtime); err != nil {
		return err
	}
	if previous != nil {
		p := previous.asset
		// Detect even a truncated filename collision before overwriting new data.
		if (p.Wasm == current.Wasm && p.WasmSHA256 != current.WasmSHA256) || (p.Runtime == current.Runtime && p.RuntimeSHA256 != current.RuntimeSHA256) {
			return errors.New("playground asset filename collision")
		}
		if p.Wasm == current.Wasm && p.Runtime != current.Runtime {
			return errors.New("identical WASM has conflicting Go runtimes")
		}
		if p != current {
			m.Previous = &p
			if err := writeFile(filepath.Join(dir, filepath.FromSlash(p.Wasm)), previous.wasm); err != nil {
				return err
			}
			if err := writeFile(filepath.Join(dir, filepath.FromSlash(p.Runtime)), previous.runtime); err != nil {
				return err
			}
		}
		// Workers from pre-manifest pages still import this stable path. New
		// workers select their matching, content-hashed runtime from the manifest.
		if err := writeFile(filepath.Join(dir, "wasm_exec.js"), previous.runtime); err != nil {
			return err
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return writeFile(filepath.Join(dir, "assets.json"), b)
}
