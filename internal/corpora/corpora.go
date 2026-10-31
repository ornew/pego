// Package corpora fetches the third-party test corpora of the ready-made parsers (parsers/) into a cache directory
// outside the repository, and finds them there.
//
// manifest.json pins every source: the URL of an archive at a fixed revision, the files to keep and the SHA-256 digest
// of what is kept. Fetching is explicit (go run ./hack/corpora fetch); tests only look in the cache and skip when a
// corpus is absent (see package corporatest).
//
// The digest is over the extracted files, not over the archive: GitHub does not promise byte-identical archives for a
// commit, while the files at a commit do not change. It is the SHA-256 of the lines "<SHA-256 of a file>  <path>\n"
// (what sha256sum prints), sorted by path.
//
// Most sources are the GitHub archive of a full commit SHA. The exception is cel-go, which is the module zip of its
// release at proxy.golang.org: a module version is immutable and recorded in the Go checksum database, so the URL
// names fixed content as a commit does, and the zip holds the test files of the release without a repository clone.
//
// Files that are already in the cache are checked against the digest before they are used (Fetch), so a changed or
// damaged entry is replaced and never trusted for its completion marker alone.
package corpora

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A Source is a third-party corpus.
type Source struct {
	Name     string   `json:"name"`
	URL      string   `json:"url"`
	Format   string   `json:"format"` // "tar.gz" (the default) or "zip"
	Revision string   `json:"revision"`
	License  string   `json:"license"`
	Strip    string   `json:"strip"`   // the leading directory of every path in a zip; a tar.gz drops its first component
	Include  []string `json:"include"` // "dir/" is a prefix, "*.ext" a suffix, anything else a file
	SHA256   string   `json:"sha256"`  // the digest of the kept files
}

//go:embed manifest.json
var manifestJSON []byte

// Manifest returns the sources of manifest.json.
func Manifest() []Source {
	var m []Source
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		panic("corpora: manifest.json: " + err.Error())
	}
	return m
}

// Lookup returns the source with the given name.
func Lookup(name string) (Source, bool) {
	for _, s := range Manifest() {
		if s.Name == name {
			return s, true
		}
	}
	return Source{}, false
}

// Root returns the cache directory: $ZP_CORPORA, or zp/corpora in the user cache directory.
func Root() (string, error) {
	if d := os.Getenv("ZP_CORPORA"); d != "" {
		return filepath.Abs(d)
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("no cache directory (set ZP_CORPORA): %w", err)
	}
	return filepath.Join(d, "zp", "corpora"), nil
}

// Dir returns the directory of the fetched files of s, and whether they are there. The directory is named by the digest
// that the manifest pins, so a changed manifest finds nothing until the new data is fetched.
func Dir(s Source) (string, bool) {
	root, err := Root()
	if err != nil {
		return "", false
	}
	dir := filepath.Join(root, s.Name, s.SHA256)
	if _, err := os.Stat(filepath.Join(dir, ".complete")); err != nil {
		return dir, false
	}
	return filepath.Join(dir, "files"), true
}

func (s Source) keeps(p string) bool {
	for _, in := range s.Include {
		switch {
		case strings.HasSuffix(in, "/"):
			if strings.HasPrefix(p, in) {
				return true
			}
		case strings.HasPrefix(in, "*"):
			if strings.HasSuffix(p, in[1:]) {
				return true
			}
		case p == in:
			return true
		}
	}
	return false
}

// A file is a kept file: its path relative to the corpus and its content.
type file struct {
	path string
	data []byte
}

// Digest returns the digest of files in the form described in the package comment.
func digest(files []file) string {
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	h := sha256.New()
	for _, f := range files {
		sum := sha256.Sum256(f.data)
		fmt.Fprintf(h, "%x  %s\n", sum, f.path)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// DigestDir returns the digest of the regular files below dir.
func DigestDir(dir string) (string, error) {
	var files []file
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		files = append(files, file{filepath.ToSlash(rel), data})
		return nil
	})
	return digest(files), err
}

// Fetch returns the directory of the verified files of s, downloading them if needed. Files that are already in the
// cache are checked against the pinned digest; an entry that does not match is removed and fetched again. A download
// whose digest differs from the manifest is an error and leaves nothing in the cache.
func Fetch(s Source) (string, error) {
	if dir, ok := Dir(s); ok {
		if Verify(s) == nil {
			return dir, nil
		}
		root, err := Root()
		if err != nil {
			return "", err
		}
		if err := os.RemoveAll(filepath.Join(root, s.Name, s.SHA256)); err != nil {
			return "", err
		}
	}
	body, err := download(s.URL)
	if err != nil {
		return "", fmt.Errorf("%s: %w", s.Name, err)
	}
	files, err := extract(s, body)
	if err != nil {
		return "", fmt.Errorf("%s: %w", s.Name, err)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("%s: no file of %s is kept", s.Name, s.URL)
	}
	if got := digest(files); got != s.SHA256 {
		return "", fmt.Errorf("%s: digest %s, the manifest pins %s", s.Name, got, s.SHA256)
	}
	return store(s, files)
}

// Pin downloads s and returns the digest of the files that it keeps, for a new entry of the manifest.
func Pin(s Source) (string, int, error) {
	body, err := download(s.URL)
	if err != nil {
		return "", 0, err
	}
	files, err := extract(s, body)
	if err != nil {
		return "", 0, err
	}
	return digest(files), len(files), nil
}

// Verify checks the files of s in the cache against the pinned digest.
func Verify(s Source) error {
	dir, ok := Dir(s)
	if !ok {
		return fmt.Errorf("%s: not fetched", s.Name)
	}
	got, err := DigestDir(dir)
	if err != nil {
		return err
	}
	if got != s.SHA256 {
		return fmt.Errorf("%s: the files in %s have digest %s, the manifest pins %s", s.Name, dir, got, s.SHA256)
	}
	return nil
}

func store(s Source, files []file) (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	parent := filepath.Join(root, s.Name)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(parent, ".fetch-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	for _, f := range files {
		p := filepath.Join(tmp, "files", filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, f.data, 0o644); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(tmp, ".complete"), []byte(s.URL+"\n"), 0o644); err != nil {
		return "", err
	}
	final := filepath.Join(parent, s.SHA256)
	if err := os.Rename(tmp, final); err != nil {
		if _, ok := Dir(s); ok { // another fetch finished first
			d, _ := Dir(s)
			return d, nil
		}
		os.RemoveAll(final) // a leftover without the marker
		if err := os.Rename(tmp, final); err != nil {
			return "", err
		}
	}
	return filepath.Join(final, "files"), nil
}

func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: 10 * time.Minute}
	var last error
	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
		resp, err := client.Get(url)
		if err != nil {
			last = err
			continue
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch {
		case err != nil:
			last = err
		case resp.StatusCode != http.StatusOK:
			last = fmt.Errorf("GET %s: %s", url, resp.Status)
			if resp.StatusCode == http.StatusNotFound {
				return nil, last
			}
		default:
			return data, nil
		}
	}
	return nil, last
}

// cleanPath returns a slash-separated relative path without ".." elements.
func cleanPath(p string) (string, bool) {
	p = path.Clean(strings.TrimPrefix(p, "./"))
	if p == "." || p == ".." || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") {
		return "", false
	}
	return p, true
}

func extract(s Source, body []byte) ([]file, error) {
	var files []file
	add := func(name string, r io.Reader) error {
		p, ok := cleanPath(name)
		if !ok || !s.keeps(p) {
			return nil
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return err
		}
		files = append(files, file{p, data})
		return nil
	}
	switch s.Format {
	case "zip":
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if !f.Mode().IsRegular() {
				continue
			}
			rest, ok := strings.CutPrefix(f.Name, s.Strip)
			if !ok {
				return nil, fmt.Errorf("zip entry %q is outside %q", f.Name, s.Strip)
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			err = add(rest, rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
		}
	case "", "tar.gz":
		gz, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			if h.Typeflag != tar.TypeReg {
				continue
			}
			_, rest, ok := strings.Cut(h.Name, "/") // the first component is the name of the checkout
			if !ok {
				continue
			}
			if err := add(rest, tr); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("unknown format %q", s.Format)
	}
	return files, nil
}
