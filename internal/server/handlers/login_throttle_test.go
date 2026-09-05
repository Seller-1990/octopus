package handlers

import "testing"

func resetLoginThrottle() {
	loginThrottleEntries.Range(func(key, _ any) bool {
		loginThrottleEntries.Delete(key)
		return true
	})
}

func TestLoginThrottleLocksAfterRepeatedFailures(t *testing.T) {
	resetLoginThrottle()
	const ip = "10.0.0.1"

	for i := 0; i < loginMaxFailures; i++ {
		if allowed, _ := loginThrottleCheck(ip); !allowed {
			t.Fatalf("attempt %d should be allowed before lockout", i+1)
		}
		loginThrottleFailure(ip)
	}

	allowed, retryAfter := loginThrottleCheck(ip)
	if allowed {
		t.Fatal("IP should be locked after reaching failure threshold")
	}
	if retryAfter <= 0 || retryAfter > loginLockoutDuration {
		t.Fatalf("retryAfter = %v, want within (0, %v]", retryAfter, loginLockoutDuration)
	}
}

func TestLoginThrottleSuccessClearsFailures(t *testing.T) {
	resetLoginThrottle()
	const ip = "10.0.0.2"

	for i := 0; i < loginMaxFailures-1; i++ {
		loginThrottleFailure(ip)
	}
	loginThrottleSuccess(ip)

	if allowed, _ := loginThrottleCheck(ip); !allowed {
		t.Fatal("successful login must clear the failure record")
	}
	// 清除后重新累计：不应立即锁定
	loginThrottleFailure(ip)
	if allowed, _ := loginThrottleCheck(ip); !allowed {
		t.Fatal("single failure after success must not trigger lockout")
	}
}

func TestLoginThrottleIPsAreIsolated(t *testing.T) {
	resetLoginThrottle()
	const lockedIP = "10.0.0.3"
	const otherIP = "10.0.0.4"

	for i := 0; i < loginMaxFailures; i++ {
		loginThrottleFailure(lockedIP)
	}
	if allowed, _ := loginThrottleCheck(lockedIP); allowed {
		t.Fatal("locked IP must be rejected")
	}
	if allowed, _ := loginThrottleCheck(otherIP); !allowed {
		t.Fatal("other IPs must not be affected by the locked IP")
	}
}
