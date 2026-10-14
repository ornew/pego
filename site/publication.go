package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// This publication prototype deliberately has no remote host adapter. An adapter
// must make CompareAndSwap atomic against every production transition, including
// manually promoted previews and rollbacks; a separate read/check/write is unsafe.
var errPublicationConflict = errors.New("published deployment changed")

type publishedDeployment struct {
	ID  string
	URL string // immutable deployment root; empty only before the first deployment
}

type publicationHost interface {
	Current(context.Context) (publishedDeployment, error)
	CompareAndSwap(context.Context, string, string) error // expected deployment ID, staged site directory
}

// promotePublication rebuilds retention at publication time, retrying a rejected
// promotion against the new authoritative predecessor. The candidate remains
// unchanged and staged output is discarded after each attempt. The host owns
// uploading a staging deployment and setting its immutable manifest source before
// atomically promoting it. Uploading without promotion must not change Current.
func promotePublication(ctx context.Context, host publicationHost, candidate string, attempts int) error {
	if attempts <= 0 {
		return errors.New("publication attempts must be positive")
	}
	// Reject a failed/incomplete build before any host operation.
	if _, err := readCandidateAssets(candidate); err != nil {
		return err
	}
	for range attempts {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := host.Current(ctx)
		if err != nil {
			return err
		}
		if (current.ID == "") != (current.URL == "") {
			return errors.New("published deployment requires both identity and immutable URL")
		}
		previous, err := readPreviousAssets(current.URL)
		if err != nil {
			return err
		}
		if current.ID != "" && previous == nil {
			return errors.New("published deployment has no parser pair")
		}
		staged, err := preparePublication(candidate, previous)
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			os.RemoveAll(staged)
			return err
		}
		err = host.CompareAndSwap(ctx, current.ID, staged)
		os.RemoveAll(staged)
		if !errors.Is(err, errPublicationConflict) {
			return err
		}
	}
	return fmt.Errorf("publication did not stabilize after %d attempts: %w", attempts, errPublicationConflict)
}

func readCandidateAssets(root string) (*assetBytes, error) {
	dir := filepath.Join(root, "playground")
	b, err := readPublicationFile(filepath.Join(dir, "assets.json"), 64<<10)
	if err != nil {
		return nil, err
	}
	var m assetManifest
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return nil, fmt.Errorf("candidate manifest: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, errors.New("trailing candidate manifest data")
	}
	if m.Version != 1 {
		return nil, fmt.Errorf("unsupported candidate manifest version %d", m.Version)
	}
	if err := m.Current.validate(); err != nil {
		return nil, err
	}
	wasm, err := readPublicationFile(filepath.Join(dir, filepath.FromSlash(m.Current.Wasm)), 64<<20)
	if err != nil {
		return nil, err
	}
	runtime, err := readPublicationFile(filepath.Join(dir, filepath.FromSlash(m.Current.Runtime)), 1<<20)
	if err != nil {
		return nil, err
	}
	if !bytes.HasPrefix(wasm, []byte{0, 'a', 's', 'm', 1, 0, 0, 0}) || len(runtime) == 0 || describeAssets(wasm, runtime) != m.Current {
		return nil, errors.New("candidate parser pair does not match its manifest")
	}
	return &assetBytes{asset: m.Current, wasm: wasm, runtime: runtime}, nil
}

func readPublicationFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("publication input is not a regular file: %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("publication file exceeds %d bytes: %s", limit, path)
	}
	return b, nil
}

// preparePublication copies a completed site to private staging, then replaces
// all generation assets with current + the actual predecessor. It reads current's
// hashed runtime, since stable wasm_exec.js can belong to a build-time predecessor.
func preparePublication(candidate string, previous *assetBytes) (staged string, err error) {
	current, err := readCandidateAssets(candidate)
	if err != nil {
		return "", err
	}
	if previous != nil {
		if err := previous.asset.validate(); err != nil {
			return "", err
		}
		if describeAssets(previous.wasm, previous.runtime) != previous.asset {
			return "", errors.New("predecessor parser pair does not match its manifest")
		}
	}
	staged, err = os.MkdirTemp("", "pego-publication-")
	if err != nil {
		return "", err
	}
	cleanupDir := staged
	defer func() {
		if err != nil {
			os.RemoveAll(cleanupDir)
		}
	}()
	err = filepath.WalkDir(candidate, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(candidate, path)
		if err != nil {
			return err
		}
		dest := filepath.Join(staged, rel)
		if entry.IsDir() {
			return os.MkdirAll(dest, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("publication input is not a regular file: %s", path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return writeFile(dest, data)
	})
	if err != nil {
		return "", err
	}
	dir := filepath.Join(staged, "playground")
	for _, sub := range []string{"wasm", "runtime"} {
		if err = os.RemoveAll(filepath.Join(dir, sub)); err != nil {
			return "", err
		}
	}
	if err = writeFile(filepath.Join(dir, current.asset.Wasm), current.wasm); err != nil {
		return "", err
	}
	if err = writeFile(filepath.Join(dir, "wasm_exec.js"), current.runtime); err != nil {
		return "", err
	}
	// The host adapter must replace the source with its new immutable staging URL.
	if err = publishAssets(dir, current.asset.Wasm, "", previous); err != nil {
		return "", err
	}
	return staged, nil
}
