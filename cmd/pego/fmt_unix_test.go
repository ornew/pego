//go:build unix

package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

func TestFmtFailedWritePreservesSource(t *testing.T) {
	for _, limit := range []int{0, 8} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			path := writeFile(t, "source.pego", messy)
			cmd := exec.Command(os.Args[0], "-test.run=^TestFmtWriteLimitHelper$")
			cmd.Env = append(os.Environ(), "PEGO_FMT_TEST_PATH="+path, "PEGO_FMT_TEST_LIMIT="+strconv.Itoa(limit))
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("child: %v\n%s", err, out)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != messy {
				t.Fatalf("failed write changed source: %q, %v", data, err)
			}
			files, err := os.ReadDir(filepath.Dir(path))
			if err != nil || len(files) != 1 || files[0].Name() != "source.pego" {
				t.Fatalf("failed write left temporary files: %v, %v", files, err)
			}
		})
	}
}

func TestFmtWriteLimitHelper(t *testing.T) {
	path := os.Getenv("PEGO_FMT_TEST_PATH")
	if path == "" {
		return
	}
	limit, err := strconv.ParseUint(os.Getenv("PEGO_FMT_TEST_LIMIT"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: limit, Max: limit}); err != nil {
		t.Fatal(err)
	}
	if err := formatFile(path, true, false, io.Discard); err == nil {
		t.Fatal("write unexpectedly succeeded despite file-size limit")
	}
}
