package update

import (
	"strings"
	"testing"
)

func TestReleaseURLsUseSellerFork(t *testing.T) {
	apiURL := latestReleaseAPIURL()
	downloadURL := releaseDownloadURL("octopus-linux-x86_64.zip")

	for _, value := range []string{apiURL, downloadURL} {
		if !strings.Contains(value, "Seller-1990/octopus") {
			t.Fatalf("release URL does not use Seller-1990 fork: %s", value)
		}
		if strings.Contains(strings.ToLower(value), "hureru/octopus") {
			t.Fatalf("release URL still points to Hureru fork: %s", value)
		}
	}
}

func TestAutoUpdateSupported(t *testing.T) {
	if autoUpdateSupported("windows") {
		t.Fatal("Windows in-place self-update must remain disabled until executable replacement is supported")
	}
	if !autoUpdateSupported("linux") {
		t.Fatal("Linux builds (including containers) should support self-update")
	}
	if !autoUpdateSupported("darwin") {
		t.Fatal("Darwin builds should support self-update")
	}
}
