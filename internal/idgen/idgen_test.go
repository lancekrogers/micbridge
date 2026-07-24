package idgen_test

import (
	"testing"

	"github.com/lancekrogers/micbridge/internal/idgen"
)

func TestSeededIsRepeatable(t *testing.T) {
	a := idgen.NewSeeded("suite")
	b := idgen.NewSeeded("suite")
	var gotA, gotB []string
	for i := 0; i < 5; i++ {
		gotA = append(gotA, a.Next("evt"))
		gotB = append(gotB, b.Next("evt"))
	}
	for i := range gotA {
		if gotA[i] != gotB[i] {
			t.Fatalf("index %d: %q != %q", i, gotA[i], gotB[i])
		}
	}
	if gotA[0] != "evt_suite_0001" {
		t.Fatalf("first id = %q", gotA[0])
	}
	a.Reset()
	if a.Next("evt") != "evt_suite_0001" {
		t.Fatal("reset did not restart sequence")
	}
}
