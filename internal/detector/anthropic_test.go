package detector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewAPIClientReadsKeyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "anthropic.key")
	t.Setenv("ADSKIP_ANTHROPIC_KEY_FILE", path)

	if _, err := newAPIClient(); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("missing file: want an error naming %s, got %v", path, err)
	}
	if err := os.WriteFile(path, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newAPIClient(); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("blank file: want an empty-key error, got %v", err)
	}
	if err := os.WriteFile(path, []byte("sk-ant-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newAPIClient(); err != nil {
		t.Fatalf("key present: %v", err)
	}
}
