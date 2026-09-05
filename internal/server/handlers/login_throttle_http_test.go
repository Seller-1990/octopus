package handlers

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestLoginThrottleHTTP(t *testing.T) {
	for _, atCapacity := range []bool{false, true} {
		t.Run(strconv.FormatBool(atCapacity), func(t *testing.T) {
			resetLoginThrottle()
			t.Cleanup(resetLoginThrottle)
			if atCapacity {
				fillLoginThrottle(t, time.Now(), loginThrottleMaxEntries)
			}
			engine := gin.New()
			if err := engine.SetTrustedProxies(nil); err != nil {
				t.Fatal(err)
			}
			engine.POST("/login", login)
			for attempt := 0; attempt <= loginMaxAttempts; attempt++ {
				request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"nonexistent-test-user","password":"test-only-password"}`))
				request.RemoteAddr = "192.0.2.1:1234"
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("X-Forwarded-For", "198.51.100."+strconv.Itoa(attempt+1))
				request.Header.Set("X-Real-IP", "203.0.113."+strconv.Itoa(attempt+1))
				recorder := httptest.NewRecorder()
				engine.ServeHTTP(recorder, request)
				want := http.StatusUnauthorized
				if atCapacity || attempt == loginMaxAttempts {
					want = http.StatusTooManyRequests
					retry, err := strconv.Atoi(recorder.Header().Get("Retry-After"))
					if err != nil || retry <= 0 {
						t.Fatalf("invalid Retry-After: %q", recorder.Header().Get("Retry-After"))
					}
				}
				if recorder.Code != want {
					t.Fatalf("attempt %d: status %d, want %d", attempt+1, recorder.Code, want)
				}
			}
		})
	}
}
