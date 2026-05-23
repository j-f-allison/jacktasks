package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDataDir(t *testing.T) {
	dir, err := DataDir()
	if err != nil {
		t.Fatalf("data dir: %v", err)
	}
	if !strings.HasSuffix(dir, "jacktasks") {
		t.Errorf("dir = %q, expected to end with 'jacktasks'", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("%q is not a directory", dir)
	}
}

func TestDBPath(t *testing.T) {
	p, err := DBPath()
	if err != nil {
		t.Fatalf("db path: %v", err)
	}
	if filepath.Base(p) != "jacktasks.db" {
		t.Errorf("base = %q, want jacktasks.db", filepath.Base(p))
	}
}