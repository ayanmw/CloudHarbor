package taskstore

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatePathRejectsJunction(t *testing.T) {
	root := t.TempDir()
	target, alias := filepath.Join(root, "target"), filepath.Join(root, "alias")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	cmd := "New-Item -ItemType Junction -Path " + quote(alias) + " -Target " + quote(target) + " -ErrorAction Stop | Out-Null"
	if out, err := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", cmd).CombinedOutput(); err != nil {
		t.Skipf("junction unavailable: %v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(alias) })
	if err := safePath(alias); err == nil {
		t.Fatal("state path accepted junction with current Go defaults")
	}
	if f, err := openJobDirectory(alias); err == nil {
		f.Close()
		t.Fatal("job directory accepted junction")
	}
}
