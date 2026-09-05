package handlers

import (
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
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
	// 容量满时的拒绝可被无凭据攻击者以海量来源 IP 持续触发,审计日志必须全局限频,否则日志量与攻击速率成正比。
	capacityDrops   uint64
	lastCapacityLog time.Time
}{entries: make(map[string]loginThrottleEntry)}

func (entry loginThrottleEntry) expired(now time.Time) bool {
	if !entry.lockedUntil.IsZero() {
		return !now.Before(entry.lockedUntil)
	}
	return !now.Before(entry.firstAttempt.Add(loginAttemptWindow))
}

func loginThrottleAttempt(ip string, now time.Time) (allowed bool, retryAfter time.Duration, atCapacity bool) {
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
		loginThrottle.capacityDrops++
		if now.Sub(loginThrottle.lastCapacityLog) >= loginThrottleSweepInterval {
			log.Warnf("SECURITY AUDIT: login throttle at capacity, rejecting unknown sources (dropped=%d since last report)", loginThrottle.capacityDrops)
			loginThrottle.capacityDrops = 0
			loginThrottle.lastCapacityLog = now
		}
		return false, loginThrottle.nextSweep.Sub(now), true
	}
	if now.Before(entry.lockedUntil) {
		return false, entry.lockedUntil.Sub(now), false
	}
	if !exists {
		entry.firstAttempt = now
	}
	entry.attempts++
	if entry.attempts == loginMaxAttempts {
		entry.lockedUntil = now.Add(loginLockoutDuration)
		log.Warnf("SECURITY AUDIT: login lockout started for IP %s for %s", ip, loginLockoutDuration)
	}
	loginThrottle.entries[ip] = entry
	return true, 0, false
}

func loginThrottleSuccess(ip string) {
	loginThrottle.Lock()
	defer loginThrottle.Unlock()
	delete(loginThrottle.entries, ip)
}
