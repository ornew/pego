package lsp

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNeovimClient edits a grammar through Neovim's built-in client, with the synchronous and the
// responsive server, and checks that the server's text follows every edit. It needs Neovim 0.11 or
// later and runs only with PEGO_LSP_NEOVIM=1.
func TestNeovimClient(t *testing.T) {
	if os.Getenv("PEGO_LSP_NEOVIM") != "1" {
		t.Skip("set PEGO_LSP_NEOVIM=1 to run Neovim")
	}
	nvim, err := exec.LookPath("nvim")
	if err != nil {
		t.Fatal(err)
	}
	script, err := filepath.Abs(filepath.Join("testdata", "neovim_client.lua"))
	if err != nil {
		t.Fatal(err)
	}
	pego := filepath.Join(t.TempDir(), "pego")
	if out, err := exec.Command("go", "build", "-o", pego, "../../cmd/pego").CombinedOutput(); err != nil {
		t.Fatalf("build pego: %v\n%s", err, out)
	}
	for _, responsive := range []string{"0", "1"} {
		t.Run("responsive="+responsive, func(t *testing.T) {
			cmd := exec.Command(nvim, "--headless", "--clean", "-l", script, t.TempDir())
			cmd.Env = append(os.Environ(), "PEGO="+pego, "RESPONSIVE="+responsive)
			out, err := cmd.CombinedOutput()
			t.Logf("%s", out)
			if err != nil || !strings.Contains(string(out), "\nPASS\n") {
				t.Fatalf("Neovim client: %v", err)
			}
		})
	}
}
