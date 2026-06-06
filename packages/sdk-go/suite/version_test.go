package suite

import "testing"

func TestVersionIsDefined(t *testing.T) {
	if Version == "" {
		t.Fatal("Version must be defined")
	}
}
