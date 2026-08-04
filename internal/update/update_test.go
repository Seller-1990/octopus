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
	if autoUpdateSupported("windows", false) {
		t.Fatal("Windows self-update must be disabled for installed desktop builds")
	}
	if autoUpdateSupported("linux", true) {
		t.Fatal("container self-update must be disabled")
	}
	if !autoUpdateSupported("linux", false) {
		t.Fatal("standalone Linux builds should support self-update")
	}
}
