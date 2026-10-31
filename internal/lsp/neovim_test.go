package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ornew/zeroparse/internal/ir/exec/core"
)

// TestNeovimClient edits a grammar through Neovim's built-in client, with the synchronous and the
// responsive server, and checks that the server's text follows every edit. It needs Neovim 0.11 or
// later and runs only with ZP_LSP_NEOVIM=1.
func TestNeovimClient(t *testing.T) {
	if os.Getenv("ZP_LSP_NEOVIM") != "1" {
		t.Skip("set ZP_LSP_NEOVIM=1 to run Neovim")
	}
	nvim, err := exec.LookPath("nvim")
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "neovim_client.lua"))
	if err != nil {
		t.Fatal(err)
	}
	zpBin := filepath.Join(t.TempDir(), "zp")
	build := exec.Command("go", "build", "-o", zpBin, "../../cmd/zp")
	build.Env = append(os.Environ(), "GOFLAGS="+core.GoFlags(os.Getenv("GOFLAGS")))
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build zp: %v\n%s", err, out)
	}
	for _, responsive := range []string{"0", "1"} {
		t.Run("responsive="+responsive, func(t *testing.T) {
			cmd := exec.Command(nvim, "--headless", "--clean", "-l", script, t.TempDir())
			cmd.Env = append(os.Environ(), "ZP="+zpBin, "RESPONSIVE="+responsive)
			out, err := cmd.CombinedOutput()
			t.Logf("%s", out)
			if err != nil || !strings.Contains(string(out), "\nPASS\n") {
				t.Fatalf("Neovim client: %v", err)
			}
		})
	}
}
