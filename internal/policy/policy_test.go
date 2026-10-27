// Package policy holds repository-wide checks that must fail closed: nothing in
// this test depends on the pipeline, it only inspects the files git tracks.
package policy

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// nativeIDs matches real platform ids: "[-]<owner>_<video>" with long numeric
// parts, and a bare channel id "-<owner>" (e.g. an -exclude-owner value).
// Placeholders in docs and tests use short letter-prefixed parts ("c1_1001").
var nativeIDs = []*regexp.Regexp{
	regexp.MustCompile(`(^|[^0-9A-Za-z.])-?[0-9]{5,}_[0-9]{6,}`),  // video id
	regexp.MustCompile(`(^|[^0-9A-Za-z_.])-[0-9]{5,}([^0-9_]|$)`), // owner id
}

// longNumberInPath catches an id baked into a generated file name; no tracked
// path needs five digits in a row.
var longNumberInPath = regexp.MustCompile(`[0-9]{5,}`)

// TestNoVideoIDsInTrackedFiles guards the data policy: video ids identify
// videos and contributors of the private product and never enter the repo.
// A .gitignore only protects "git add ."; this also catches "-f", renamed
// directories and generated output written into a committed path.
func TestNoVideoIDsInTrackedFiles(t *testing.T) {
	root := repoRoot(t)
	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("git ls-files: %v", err)
	}
	var hits []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		if longNumberInPath.MatchString(rel) {
			hits = append(hits, rel+" (file name)")
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue // deleted but still tracked: nothing to leak
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, re := range nativeIDs {
				if re.MatchString(line) {
					hits = append(hits, rel+":"+strconv.Itoa(i+1))
				}
			}
		}
	}
	if len(hits) > 0 {
		t.Fatalf("video ids in tracked files (data policy):\n  %s", strings.Join(hits, "\n  "))
	}
}

// urlHost matches the host of any absolute URL in a tracked file.
var urlHost = regexp.MustCompile(`https?://([A-Za-z0-9.-]+)`)

// TestNoPlatformURLsInTrackedFiles keeps the repo from naming the platform it
// runs against: every platform URL is a template in the providers config
// outside the tree, and examples use example.invalid.
func TestNoPlatformURLsInTrackedFiles(t *testing.T) {
	allowed := map[string]bool{
		"example.invalid": true, // placeholders in docs and tests
		"www.w3.org":      true, // SVG namespace
		"github.com":      true, // module path
		"ifconfig.me":     true, // netguard external-IP lookup
	}
	root := repoRoot(t)
	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Skipf("git ls-files: %v", err)
	}
	var hits []string
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel == "" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, m := range urlHost.FindAllStringSubmatch(line, -1) {
				if !allowed[m[1]] {
					hits = append(hits, rel+":"+strconv.Itoa(i+1)+" "+m[1])
				}
			}
		}
	}
	if len(hits) > 0 {
		t.Fatalf("platform URLs in tracked files (they belong in providers.json):\n  %s", strings.Join(hits, "\n  "))
	}
}

func repoRoot(t *testing.T) string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	return strings.TrimSpace(string(out))
}
