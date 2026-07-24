package api

import "testing"

func TestCurrentStatus(t *testing.T) {
	st := CurrentStatus()
	if st.Name != "micbridge" {
		t.Fatalf("Name = %q", st.Name)
	}
	if st.Version == "" {
		t.Fatal("Version must not be empty")
	}
	if !st.Healthy {
		t.Fatal("bootstrap status should be healthy")
	}
}
