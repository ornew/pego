package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeBootstrapStub(t *testing.T, dir, name, body string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestToolchainBootstrapRejectsUnsupportedArchitectureBeforeDownload(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	tempDir := filepath.Join(root, "tmp")
	cacheDir := filepath.Join(root, "cache")
	for _, dir := range []string{binDir, tempDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(root, "curl-called")
	writeBootstrapStub(t, binDir, "uname", `if [ "$1" = -m ]; then echo aarch64; else echo Linux; fi`)
	writeBootstrapStub(t, binDir, "curl", `echo called > "$CURL_MARKER"`)

	cmd := exec.Command("sh", "install-playground-toolchains.sh")
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"TMPDIR="+tempDir,
		"NETLIFY_CACHE_DIR="+cacheDir,
		"CURL_MARKER="+marker,
	)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "supports x86_64 only") {
		t.Fatalf("bootstrap did not reject aarch64 before installation: err=%v output=%s", err, out)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("curl ran before architecture rejection: stat err=%v", err)
	}
}

func TestToolchainBootstrapStopsOnChecksumMismatchBeforeExtraction(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	tempDir := filepath.Join(root, "tmp")
	cacheDir := filepath.Join(root, "cache")
	for _, dir := range []string{binDir, tempDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tarMarker := filepath.Join(root, "tar-called")
	writeBootstrapStub(t, binDir, "uname", `if [ "$1" = -m ]; then echo x86_64; else echo Linux; fi`)
	writeBootstrapStub(t, binDir, "curl", `while [ "$#" -gt 0 ]; do if [ "$1" = --output ]; then shift; printf 'invalid archive' > "$1"; exit 0; fi; shift; done; exit 2`)
	writeBootstrapStub(t, binDir, "tar", `echo called > "$TAR_MARKER"`)

	cmd := exec.Command("sh", "install-playground-toolchains.sh")
	cmd.Env = append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"TMPDIR="+tempDir,
		"NETLIFY_CACHE_DIR="+cacheDir,
		"TAR_MARKER="+tarMarker,
	)
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "SHA-256 mismatch") {
		t.Fatalf("bootstrap did not reject a checksum mismatch: err=%v output=%s", err, out)
	}
	if _, err := os.Stat(tarMarker); !os.IsNotExist(err) {
		t.Fatalf("archive extraction started before checksum verification: stat err=%v", err)
	}
}
