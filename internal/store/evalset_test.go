package store

import "testing"

func TestBucketIsStableAndSpread(t *testing.T) {
	if Bucket("main", "c1_1001", 5) != Bucket("main", "c1_1001", 5) {
		t.Fatal("bucket must be deterministic")
	}
	seen := map[uint64]int{}
	for i := 0; i < 1000; i++ {
		seen[Bucket("main", "c1_"+itoa(1000+i), 5)]++
	}
	for b := uint64(0); b < 5; b++ {
		if seen[b] < 150 || seen[b] > 250 { // 200 expected
			t.Fatalf("bucket %d has %d of 1000: not spread", b, seen[b])
		}
	}
}

func itoa(i int) string {
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
