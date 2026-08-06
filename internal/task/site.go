package task

import (
	"context"
	"time"

	"github.com/bestruirui/octopus/internal/site"
	"github.com/bestruirui/octopus/internal/utils/log"
)

func SiteSyncTask() {
	log.Debugf("site sync task started")
	startTime := time.Now()
	defer func() {
		log.Debugf("site sync task finished, update time: %s", time.Since(startTime))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	site.SyncAll(ctx)
}

func SiteCheckinTask() {
	log.Debugf("site checkin task started")
	startTime := time.Now()
	defer func() {
		log.Debugf("site checkin task finished, update time: %s", time.Since(startTime))
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	site.CheckinAll(ctx)
}

func Sub2APIRefreshTask() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	summary, err := site.RefreshDueSub2APIAccounts(ctx, 4)
	if err != nil {
		log.Warnf("sub2api refresh task failed: %v", err)
		return
	}
	log.Debugf("sub2api refresh task finished: selected=%d succeeded=%d failed=%d", summary.Selected, summary.Succeeded, summary.Failed)
}
