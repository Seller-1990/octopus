package update

import (
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
)

var (
	latestVersionCache   string
	latestVersionCacheMu sync.RWMutex
	backgroundStarted    bool
)

// LatestVersionCached returns the most recent cached version from background checks.
func LatestVersionCached() string {
	latestVersionCacheMu.RLock()
	defer latestVersionCacheMu.RUnlock()
	return latestVersionCache
}

// StartBackgroundCheck starts a goroutine that checks for updates every 6 hours.
func StartBackgroundCheck() {
	if backgroundStarted {
		return
	}
	backgroundStarted = true
	go func() {
		check()
		ticker := time.NewTicker(6 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			check()
		}
	}()
}

func check() {
	info, err := GetLatestInfo()
	if err != nil {
		log.Debugf("background update check failed: %v", err)
		return
	}
	latestVersionCacheMu.Lock()
	latestVersionCache = info.TagName
	latestVersionCacheMu.Unlock()
	log.Debugf("background update check: latest=%s", info.TagName)
}
