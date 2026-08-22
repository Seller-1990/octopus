package safe

import (
	"runtime/debug"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
)

// Go runs fn in a goroutine and guarantees panic recovery.
func Go(name string, fn func()) {
	go func() {
		Run(name, fn)
	}()
}

// Run executes fn and converts panics into logs instead of process exits.
func Run(name string, fn func()) {
	start := time.Now()
	defer func() {
		if r := recover(); r != nil {
			log.Errorf("panic recovered (%s): %v\n%s", name, r, debug.Stack())
			return
		}
		log.Debugf("safe run finished (%s), elapsed=%s", name, time.Since(start))
	}()

	if fn == nil {
		log.Warnf("safe run skipped (%s): fn is nil", name)
		return
	}

	fn()
}
