package handlers

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func resetLoginThrottle() {
	loginThrottle.Lock()
	defer loginThrottle.Unlock()
	clear(loginThrottle.entries)
	loginThrottle.nextSweep = time.Time{}
}

func TestLoginThrottleLocksAfterRepeatedAttempts(t *testing.T) {
	resetLoginThrottle()
	const ip = "10.0.0.1"
	now := time.Now()

	for attempt := 0; attempt < loginMaxAttempts; attempt++ {
		if allowed, _ := loginThrottleAttempt(ip, now); !allowed {
			t.Fatalf("attempt %d should be allowed before lockout", attempt+1)
		}
	}

	allowed, retryAfter := loginThrottleAttempt(ip, now)
	if allowed {
		t.Fatal("IP should be locked after exhausting the attempt budget")
	}
	if retryAfter != loginLockoutDuration {
		t.Fatalf("retryAfter = %v, want %v", retryAfter, loginLockoutDuration)
	}
}

func TestLoginThrottleSuccessClearsAttempts(t *testing.T) {
	resetLoginThrottle()
	const ip = "10.0.0.2"
	now := time.Now()

	for attempt := 0; attempt < loginMaxAttempts; attempt++ {
		loginThrottleAttempt(ip, now)
	}
	loginThrottleSuccess(ip)

	for attempt := 0; attempt < loginMaxAttempts; attempt++ {
		if allowed, _ := loginThrottleAttempt(ip, now); !allowed {
			t.Fatal("successful login must reset the full attempt budget, including an in-flight lock")
		}
	}
}

func TestLoginThrottleIPsAreIsolated(t *testing.T) {
	resetLoginThrottle()
	const lockedIP = "10.0.0.3"
	const otherIP = "10.0.0.4"
	now := time.Now()

	for attempt := 0; attempt < loginMaxAttempts; attempt++ {
		loginThrottleAttempt(lockedIP, now)
	}
	if allowed, _ := loginThrottleAttempt(lockedIP, now); allowed {
		t.Fatal("locked IP must be rejected")
	}
	if allowed, _ := loginThrottleAttempt(otherIP, now); !allowed {
		t.Fatal("other IPs must not be affected by the locked IP")
	}
}

func TestLoginThrottleConcurrentAccess(t *testing.T) {
	resetLoginThrottle()
	var workers sync.WaitGroup
	start := make(chan struct{})
	for worker := 0; worker < 12; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			<-start
			for attempt := 0; attempt < 20; attempt++ {
				allowed, _ := loginThrottleAttempt("192.0.2.1", time.Now())
				if allowed && worker%3 == 0 {
					loginThrottleSuccess("192.0.2.1")
				}
			}
		}(worker)
	}
	close(start)
	workers.Wait()
}

func TestLoginThrottleConcurrentAdmissionIsBounded(t *testing.T) {
	resetLoginThrottle()
	var workers sync.WaitGroup
	var admitted atomic.Int32
	start := make(chan struct{})
	for worker := 0; worker < 32; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			if allowed, _ := loginThrottleAttempt("192.0.2.2", time.Now()); allowed {
				admitted.Add(1)
			}
		}()
	}
	close(start)
	workers.Wait()
	if admitted.Load() != loginMaxAttempts {
		t.Fatalf("admitted %d concurrent attempts, want %d", admitted.Load(), loginMaxAttempts)
	}
}

func TestLoginThrottleExpiryBoundaries(t *testing.T) {
	now := time.Now()
	for _, scenario := range []struct {
		name  string
		entry loginThrottleEntry
		until time.Time
	}{
		{name: "attempt window", entry: loginThrottleEntry{attempts: 2, firstAttempt: now}, until: now.Add(loginAttemptWindow)},
		{name: "lock outlasts window", entry: loginThrottleEntry{attempts: loginMaxAttempts, firstAttempt: now, lockedUntil: now.Add(loginLockoutDuration)}, until: now.Add(loginLockoutDuration)},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			for _, offset := range []time.Duration{-time.Nanosecond, 0, time.Nanosecond} {
				if expired := scenario.entry.expired(scenario.until.Add(offset)); expired != (offset >= 0) {
					t.Fatalf("expiry at offset %s = %v", offset, expired)
				}
			}
			resetLoginThrottle()
			loginThrottle.entries["expired"] = scenario.entry
			if allowed, _ := loginThrottleAttempt("expired", scenario.until); !allowed {
				t.Fatal("expired budget must admit a fresh attempt")
			}
			if entry := loginThrottle.entries["expired"]; entry.attempts != 1 || !entry.lockedUntil.IsZero() {
				t.Fatalf("expired entry was not reset: %+v", entry)
			}
		})
	}
}

func TestLoginThrottleSweepsOtherIPsWithoutExpiringActiveLocks(t *testing.T) {
	resetLoginThrottle()
	now := time.Now()
	stale := loginThrottleEntry{attempts: 1, firstAttempt: now.Add(-loginAttemptWindow)}
	locked := loginThrottleEntry{attempts: loginMaxAttempts, firstAttempt: now.Add(-2 * loginAttemptWindow), lockedUntil: now.Add(2 * loginThrottleSweepInterval)}
	loginThrottle.entries["stale"] = stale
	loginThrottle.entries["locked"] = locked
	loginThrottle.entries["recent"] = loginThrottleEntry{attempts: 1, firstAttempt: now}
	loginThrottleAttempt("new", now)
	if _, exists := loginThrottle.entries["stale"]; exists {
		t.Fatal("another IP must trigger reclamation of expired entries")
	}
	if loginThrottle.entries["locked"] != locked || len(loginThrottle.entries) != 3 {
		t.Fatal("cleanup must preserve active locks and unexpired budgets")
	}
	loginThrottle.entries["stale"] = stale
	loginThrottleAttempt("new", now.Add(loginThrottleSweepInterval/2))
	if _, exists := loginThrottle.entries["stale"]; !exists {
		t.Fatal("cleanup must not rescan all entries on every request")
	}
	loginThrottleAttempt("new", now.Add(loginThrottleSweepInterval))
	if _, exists := loginThrottle.entries["stale"]; exists {
		t.Fatal("cleanup must run at the next sweep boundary")
	}
	if loginThrottle.entries["locked"] != locked {
		t.Fatal("denials and sweeps must neither extend nor remove an active lock")
	}
}

func fillLoginThrottle(t *testing.T, now time.Time, count int) {
	t.Helper()
	for index := 0; index < count; index++ {
		if allowed, _ := loginThrottleAttempt(fmt.Sprintf("peer-%d", index), now); !allowed {
			t.Fatalf("entry %d rejected before capacity", index)
		}
	}
}

func TestLoginThrottleCapacityFailsClosed(t *testing.T) {
	resetLoginThrottle()
	now := time.Now()
	fillLoginThrottle(t, now, loginThrottleMaxEntries)
	for attempt := 1; attempt < loginMaxAttempts; attempt++ {
		loginThrottleAttempt("peer-0", now)
	}
	locked := loginThrottle.entries["peer-0"]
	loginThrottle.entries["peer-2"] = loginThrottleEntry{firstAttempt: now.Add(-loginAttemptWindow), attempts: 1}
	for index := 0; index < 20; index++ {
		allowed, retry := loginThrottleAttempt(fmt.Sprintf("overflow-%d", index), now)
		if allowed || retry != loginThrottleSweepInterval || len(loginThrottle.entries) != loginThrottleMaxEntries {
			t.Fatal("full capacity must reject unknown IPs without growing or evicting entries")
		}
	}
	if loginThrottle.entries["peer-0"] != locked || loginThrottle.entries["peer-1"].attempts != 1 {
		t.Fatal("capacity pressure must preserve existing locks and attempt counts")
	}
	if allowed, retry := loginThrottleAttempt("peer-0", now.Add(time.Second)); allowed || retry != loginLockoutDuration-time.Second {
		t.Fatal("a rejected request must not extend the lockout")
	}
	if allowed, _ := loginThrottleAttempt("peer-1", now); !allowed {
		t.Fatal("existing IPs must retain their remaining budget at capacity")
	}
	loginThrottleSuccess("peer-1")
	if allowed, _ := loginThrottleAttempt("last-slot", now); !allowed {
		t.Fatal("a successful login must free capacity")
	}
	if allowed, _ := loginThrottleAttempt("next-slot", now); allowed {
		t.Fatal("admission must reserve the last slot before credential verification")
	}
	if allowed, _ := loginThrottleAttempt("after-sweep", now.Add(loginThrottleSweepInterval)); !allowed {
		t.Fatal("capacity must recover when the sweep reclaims expired entries")
	}
}

func TestLoginThrottleConcurrentCapacity(t *testing.T) {
	resetLoginThrottle()
	now := time.Now()
	fillLoginThrottle(t, now, loginThrottleMaxEntries-1)
	var workers sync.WaitGroup
	var admitted atomic.Int32
	start := make(chan struct{})
	for index := 0; index < 32; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			if allowed, _ := loginThrottleAttempt(fmt.Sprintf("new-peer-%d", index), now); allowed {
				admitted.Add(1)
			}
		}(index)
	}
	close(start)
	workers.Wait()
	if admitted.Load() != 1 || len(loginThrottle.entries) != loginThrottleMaxEntries {
		t.Fatalf("last slot admitted %d peers with %d entries", admitted.Load(), len(loginThrottle.entries))
	}
}
