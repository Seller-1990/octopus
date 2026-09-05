package handlers

import (
	"sync"
	"time"
)

const (
	loginMaxAttempts           = 5
	loginAttemptWindow         = 10 * time.Minute
	loginLockoutDuration       = 15 * time.Minute
	loginThrottleMaxEntries    = 10000
	loginThrottleSweepInterval = time.Minute
)

type loginThrottleEntry struct {
	attempts     int
	firstAttempt time.Time
	lockedUntil  time.Time
}

var loginThrottle = struct {
	sync.Mutex
	entries   map[string]loginThrottleEntry
	nextSweep time.Time
}{entries: make(map[string]loginThrottleEntry)}

func (entry loginThrottleEntry) expired(now time.Time) bool {
	if !entry.lockedUntil.IsZero() {
		return !now.Before(entry.lockedUntil)
	}
	return !now.Before(entry.firstAttempt.Add(loginAttemptWindow))
}

func loginThrottleAttempt(ip string, now time.Time) (bool, time.Duration) {
	loginThrottle.Lock()
	defer loginThrottle.Unlock()
	if !now.Before(loginThrottle.nextSweep) {
		for key, entry := range loginThrottle.entries {
			if entry.expired(now) {
				delete(loginThrottle.entries, key)
			}
		}
		loginThrottle.nextSweep = now.Add(loginThrottleSweepInterval)
	}
	entry, exists := loginThrottle.entries[ip]
	if exists && entry.expired(now) {
		delete(loginThrottle.entries, ip)
		entry = loginThrottleEntry{}
		exists = false
	}
	if !exists && len(loginThrottle.entries) >= loginThrottleMaxEntries {
		return false, loginThrottle.nextSweep.Sub(now)
	}
	if now.Before(entry.lockedUntil) {
		return false, entry.lockedUntil.Sub(now)
	}
	if !exists {
		entry.firstAttempt = now
	}
	entry.attempts++
	if entry.attempts == loginMaxAttempts {
		entry.lockedUntil = now.Add(loginLockoutDuration)
	}
	loginThrottle.entries[ip] = entry
	return true, 0
}

func loginThrottleSuccess(ip string) {
	loginThrottle.Lock()
	defer loginThrottle.Unlock()
	delete(loginThrottle.entries, ip)
}
