package sheets

import (
	"bytes"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	sealed, err := seal(key, []byte("refresh-token"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("refresh-token")) {
		t.Fatal("sealed value contains the plaintext")
	}
	got, err := open(key, sealed)
	if err != nil || string(got) != "refresh-token" {
		t.Fatalf("open = %q, %v", got, err)
	}

	if _, err := open(bytes.Repeat([]byte{8}, 32), sealed); err == nil {
		t.Fatal("open with the wrong key succeeded")
	}
	sealed[len(sealed)-1] ^= 1
	if _, err := open(key, sealed); err == nil {
		t.Fatal("open of tampered data succeeded")
	}
}

func TestRowNumberFindsLeadInColumnA(t *testing.T) {
	colA := [][]any{{"Lead ID"}, {"p-1"}, {}, {"p-3"}}
	cases := map[string]int{"p-1": 2, "p-3": 4, "p-9": 0, "Lead ID": 0}
	for id, want := range cases {
		if got := rowNumber(colA, id); got != want {
			t.Errorf("rowNumber(%q) = %d, want %d", id, got, want)
		}
	}
}
