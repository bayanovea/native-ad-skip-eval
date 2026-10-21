package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestImportIsIdempotent(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "adskip.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	d := 1200.0
	videos := []Video{{Provider: "main", NativeID: "c1_1001"}}
	contribs := []Contributor{{ID: "aaaa", IsAssessor: true}}
	labels := []Label{
		{Provider: "main", NativeID: "c1_1001", Contributor: "aaaa", Category: "sponsor", Start: 10, End: 70},
		{Provider: "main", NativeID: "c1_1001", Contributor: "aaaa", Category: "intro", Start: 0, End: 12},
	}
	st, err := s.Import(ctx, videos, contribs, labels)
	if err != nil {
		t.Fatal(err)
	}
	if st.Labels != 2 || st.Duplicates != 0 {
		t.Fatalf("first import: %+v", st)
	}
	// Second run: same labels, duration now known.
	videos[0].Duration = &d
	st, err = s.Import(ctx, videos, contribs, labels)
	if err != nil {
		t.Fatal(err)
	}
	if st.Labels != 0 || st.Duplicates != 2 {
		t.Fatalf("second import: %+v", st)
	}
	v, c, l, err := s.Counts(ctx)
	if err != nil || v != 1 || c != 1 || l != 2 {
		t.Fatalf("counts %d %d %d %v", v, c, l, err)
	}
	var got float64
	if err := s.DB.QueryRow(`SELECT duration FROM videos`).Scan(&got); err != nil || got != 1200 {
		t.Fatalf("duration not updated: %v %v", got, err)
	}
	// Third run with a NULL duration must not erase the known one.
	videos[0].Duration = nil
	if _, err := s.Import(ctx, videos, contribs, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.DB.QueryRow(`SELECT duration FROM videos`).Scan(&got); err != nil || got != 1200 {
		t.Fatalf("duration erased by NULL: %v %v", got, err)
	}
}

func TestLabelNeedsKnownVideo(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "adskip.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, err = s.Import(context.Background(), nil, []Contributor{{ID: "aaaa"}},
		[]Label{{Provider: "main", NativeID: "c1_1009", Contributor: "aaaa", Category: "sponsor", Start: 1, End: 2}})
	if err == nil {
		t.Fatal("expected a foreign key error for a label without its video")
	}
}

func TestSetFlagsKeepsNewest(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "adskip.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	newer := VideoFlag{Provider: "main", NativeID: "c1_1001", Flag: "track_mismatch", Value: true, Reviewer: "a", SetAt: "2026-10-20T10:00:00Z"}
	older := newer
	older.Value, older.SetAt = false, "2026-10-19T10:00:00Z"
	if n, err := s.SetFlags(ctx, []VideoFlag{newer}); err != nil || n != 1 {
		t.Fatalf("first import: %d %v", n, err)
	}
	if n, err := s.SetFlags(ctx, []VideoFlag{older}); err != nil || n != 0 {
		t.Fatalf("older value must not replace newer: %d %v", n, err)
	}
	got, err := s.Flags(ctx, "track_mismatch")
	if err != nil || len(got) != 1 || !got["main:c1_1001"] {
		t.Fatalf("flags %v %v", got, err)
	}
	later := newer
	later.Value, later.SetAt = false, "2026-10-21T10:00:00Z"
	if _, err := s.SetFlags(ctx, []VideoFlag{later}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Flags(ctx, "track_mismatch"); got["main:c1_1001"] {
		t.Fatalf("newer false not applied: %v", got)
	}
}
