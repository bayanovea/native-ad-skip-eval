// Package netguard makes sure data collection never runs from a listed "home" IP.
// It fails closed: no list, unreadable list, or no IP lookup means no network.
package netguard

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// DefaultLookupURL returns the caller's external IPv4/IPv6 as plain text.
const DefaultLookupURL = "https://ifconfig.me/ip"

// ErrRefused is returned when the external IP is one of the listed home IPs.
var ErrRefused = errors.New("netguard: external IP is a listed home IP; turn the VPN on")

// LoadHomeIPs reads one IP per line; blank lines and # comments are ignored.
// A missing or empty file is an error, by design.
func LoadHomeIPs(path string) ([]net.IP, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("netguard: home IP list: %w (create it with one IP per line)", err)
	}
	defer f.Close()
	var ips []net.IP
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ip := net.ParseIP(line)
		if ip == nil {
			return nil, fmt.Errorf("netguard: %s: not an IP: %q", path, line)
		}
		ips = append(ips, ip)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("netguard: %s lists no IPs; refusing to run blind", path)
	}
	return ips, nil
}

// ExternalIP asks lookupURL for the current public IP.
func ExternalIP(ctx context.Context, lookupURL string) (net.IP, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lookupURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "curl/8") // ifconfig.me serves HTML to browsers
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("netguard: IP lookup: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(strings.TrimSpace(string(body)))
	if ip == nil {
		return nil, fmt.Errorf("netguard: IP lookup returned %q", strings.TrimSpace(string(body)))
	}
	return ip, nil
}

// Check returns ErrRefused if ip is in home.
func Check(ip net.IP, home []net.IP) error {
	for _, h := range home {
		if h.Equal(ip) {
			return fmt.Errorf("%w (%s)", ErrRefused, ip)
		}
	}
	return nil
}

// Verify loads the list, looks the IP up and checks it. Returns the IP on success.
func Verify(ctx context.Context, homeListPath, lookupURL string) (net.IP, error) {
	home, err := LoadHomeIPs(homeListPath)
	if err != nil {
		return nil, err
	}
	ip, err := ExternalIP(ctx, lookupURL)
	if err != nil {
		return nil, err
	}
	return ip, Check(ip, home)
}
