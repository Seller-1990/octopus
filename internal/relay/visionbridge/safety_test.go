package visionbridge

import (
	"encoding/base64"
	"strings"
	"testing"
)

func validDataURI(payloadBytes int) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, payloadBytes))
}

func TestValidateImageReferenceDataURI(t *testing.T) {
	size, err := ValidateImageReference(validDataURI(1024), 4096)
	if err != nil {
		t.Fatalf("valid data URI rejected: %v", err)
	}
	if size < 1024 {
		t.Fatalf("estimated size %d < payload size 1024", size)
	}

	if _, err := ValidateImageReference(validDataURI(8192), 4096); err == nil {
		t.Fatal("oversized data URI accepted")
	}
	if _, err := ValidateImageReference("data:image/png;base64,!!!!", 4096); err == nil {
		t.Fatal("invalid base64 accepted")
	}
	if _, err := ValidateImageReference("data:image/*;base64,AAAA", 4096); err == nil {
		t.Fatal("wildcard media type accepted")
	}
	if _, err := ValidateImageReference("data:application/pdf;base64,AAAA", 4096); err == nil {
		t.Fatal("non-image media type accepted")
	}
	if _, err := ValidateImageReference("data:image/png,plaintext", 4096); err == nil {
		t.Fatal("non-base64 data URI accepted")
	}
}

func TestValidateImageReferenceURL(t *testing.T) {
	if _, err := ValidateImageReference("https://example.com/cat.jpg", 0); err != nil {
		t.Fatalf("valid https URL rejected: %v", err)
	}
	if _, err := ValidateImageReference("http://example.com/cat.jpg", 0); err != nil {
		t.Fatalf("valid http URL rejected: %v", err)
	}

	rejected := []string{
		"file:///etc/passwd",
		"ftp://example.com/cat.jpg",
		"https://localhost/cat.jpg",
		"https://foo.localhost/cat.jpg",
		"https://printer.local/cat.jpg",
		"https://127.0.0.1/cat.jpg",
		"https://[::1]/cat.jpg",
		"https://10.0.0.8/cat.jpg",
		"https://192.168.50.139:8088/cat.jpg",
		"https://169.254.1.1/cat.jpg",
		"https://0.0.0.0/cat.jpg",
		"https://user:pass@example.com/cat.jpg",
	}
	for _, ref := range rejected {
		if _, err := ValidateImageReference(ref, 0); err == nil {
			t.Errorf("forbidden reference accepted: %s", ref)
		}
	}
}

func TestValidateImageReferencePublicIPAllowed(t *testing.T) {
	if _, err := ValidateImageReference("https://93.184.216.34/cat.jpg", 0); err != nil {
		t.Fatalf("public IP rejected: %v", err)
	}
}

func TestValidateImageReferenceErrorMentionsLimit(t *testing.T) {
	_, err := ValidateImageReference(validDataURI(8192), 4096)
	if err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("expected size limit error, got: %v", err)
	}
}
