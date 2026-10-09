package netguard

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeList(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "home_ips")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestVerifyRefusesHomeIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("203.0.113.7\n"))
	}))
	defer srv.Close()
	list := writeList(t, "# home\n203.0.113.7\n")
	_, err := Verify(context.Background(), list, srv.URL)
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("want ErrRefused, got %v", err)
	}
}

func TestVerifyPassesOtherIP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte("198.51.100.1"))
	}))
	defer srv.Close()
	list := writeList(t, "203.0.113.7\n")
	ip, err := Verify(context.Background(), list, srv.URL)
	if err != nil || !ip.Equal(net.ParseIP("198.51.100.1")) {
		t.Fatalf("got %v, %v", ip, err)
	}
}

func TestFailsClosed(t *testing.T) {
	if _, err := LoadHomeIPs(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("missing list must be an error")
	}
	if _, err := LoadHomeIPs(writeList(t, "# nothing\n")); err == nil {
		t.Error("empty list must be an error")
	}
	if _, err := LoadHomeIPs(writeList(t, "not-an-ip\n")); err == nil {
		t.Error("garbage must be an error")
	}
}
