package gold

import (
	"strings"
	"testing"
)

// Placeholder ids only (short owner part), see the data policy.
const sample = `
{"provider":"main","native_id":"c1_1001","duration":1200,"category":"sponsor","start":10,"end":70,"contributor":"aaaa","is_assessor":true,"contributor_rating":70,"contributor_banned":false,"created_at":"2026-08-01T00:00:00Z"}
{"provider":"main","native_id":"c1_1001","duration":null,"category":"intro","start":0,"end":12,"contributor":"bbbb","is_assessor":false,"contributor_rating":0,"contributor_banned":false,"created_at":"2026-08-02T00:00:00Z"}
{"provider":"main","native_id":"c1_1002","duration":null,"category":"credits","start":500,"end":480,"contributor":"bbbb","is_assessor":false,"contributor_rating":0,"contributor_banned":false,"created_at":"2026-08-02T00:00:00Z"}
{"provider":"main","native_id":"c1_1002","duration":900,"category":"ads","start":1,"end":2,"contributor":"bbbb","is_assessor":false,"contributor_rating":0,"contributor_banned":false,"created_at":"2026-08-02T00:00:00Z"}
not json
`

func TestRead(t *testing.T) {
	b, err := Read(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if b.Read != 5 || len(b.Rejected) != 3 || len(b.Labels) != 2 {
		t.Fatalf("read=%d rejected=%v labels=%d", b.Read, b.Rejected, len(b.Labels))
	}
	for _, want := range []string{"line 4: end 480 <= start 500", "line 5: unknown category", "line 6:"} {
		if !strings.Contains(strings.Join(b.Rejected, "\n"), want) {
			t.Errorf("missing rejection %q in %v", want, b.Rejected)
		}
	}
	if len(b.Videos) != 1 || b.Videos[0].Duration == nil || *b.Videos[0].Duration != 1200 {
		t.Fatalf("videos: %+v (rejected rows must not create videos; duration kept from the row that has it)", b.Videos)
	}
	if len(b.Contributors) != 2 || !b.Contributors[0].IsAssessor || b.Contributors[1].IsAssessor {
		t.Fatalf("contributors: %+v", b.Contributors)
	}
}
