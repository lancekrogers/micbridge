//go:build linux

package hollyland35470007_test

import (
	"context"
	"os"
	"testing"

	hl "github.com/lancekrogers/micbridge/internal/driver/hollyland35470007"
)

func TestOptInLiveDiscoveryMatchesProfile(t *testing.T) {
	if os.Getenv("MICBRIDGE_HIL") != "1" {
		t.Skip("set MICBRIDGE_HIL=1 with an attached 3547:0007 receiver")
	}
	candidates, err := hl.DiscoverLinux(context.Background(), "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("want one exact physical candidate, got %d", len(candidates))
	}
	match, err := hl.NewWithIdentitySalt(nil, []byte("hil-test")).Match(context.Background(), candidates[0])
	if err != nil {
		t.Fatalf("sanitized candidate did not match: %v", err)
	}
	if match.Confidence != "exact" {
		t.Fatalf("match=%+v", match)
	}
}
