package policy

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// productPackages are copied into the product backend by its scripts/sync-detector.sh,
// which rewrites only the imports between them.
var productPackages = []string{"detector", "transcript", "vtt", "provider", "ytdlp"}

// TestProductPackagesImportOnlyEachOther keeps the copied packages free of the
// eval-only code (store, netguard, gold, ...): they may import the standard
// library, each other and the Anthropic SDK, nothing else.
func TestProductPackagesImportOnlyEachOther(t *testing.T) {
	root := repoRoot(t)
	allowed := map[string]bool{}
	for _, p := range productPackages {
		allowed["github.com/bayanovea/ad-skip-segment-eval/internal/"+p] = true
	}
	var hits []string
	for _, p := range productPackages {
		dir := filepath.Join(root, "internal", p)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".go") {
				continue
			}
			f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				path, _ := strconv.Unquote(imp.Path.Value)
				stdlib := !strings.Contains(strings.SplitN(path, "/", 2)[0], ".")
				if !stdlib && !allowed[path] && !strings.HasPrefix(path, "github.com/anthropics/anthropic-sdk-go") {
					hits = append(hits, "internal/"+p+"/"+e.Name()+": "+path)
				}
			}
		}
	}
	if len(hits) > 0 {
		t.Fatalf("product packages import eval-only code:\n  %s", strings.Join(hits, "\n  "))
	}
}
