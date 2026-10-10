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

// nativeIDs matches real platform video ids. "-<owner>_<video>" with long
// numeric parts. Placeholders in docs use short owners ("c1_1001") on purpose.
var nativeIDs = []*regexp.Regexp{
	regexp.MustCompile(`(^|[^0-9A-Za-z])-[0-9]{5,}_[0-9]{6,}`), // video id
}

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

func repoRoot(t *testing.T) string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	return strings.TrimSpace(string(out))
}
