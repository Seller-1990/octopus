package handlers

import (
	"sync"
	"time"

	"github.com/bestruirui/octopus/internal/utils/log"
)

// 登录口令是自托管面板的唯一防线，必须阻断无限制的暴力穷举。
// 阈值与锁定时长取保守值：内网反代场景下所有请求的 ClientIP 可能相同，
// 锁定时长不宜过长（管理员连错 5 次等 15 分钟可接受），到期自动解锁。
const (
	loginMaxFailures     = 5
	loginFailureWindow   = 10 * time.Minute
	loginLockoutDuration = 15 * time.Minute
)

type loginThrottleEntry struct {
	failures    int
	firstFail   time.Time
	lockedUntil time.Time
}

var loginThrottleEntries sync.Map // ip -> *loginThrottleEntry

// loginThrottleCheck 返回该 IP 是否允许发起登录尝试；
// 被锁定时返回需等待的剩余时长。
func loginThrottleCheck(ip string) (bool, time.Duration) {
	value, ok := loginThrottleEntries.Load(ip)
	if !ok {
		return true, 0
	}
	entry := value.(*loginThrottleEntry)
	now := time.Now()
	if now.Before(entry.lockedUntil) {
		return false, entry.lockedUntil.Sub(now)
	}
	// 未锁定且失败窗口已过期：重置计数
	if entry.failures > 0 && now.Sub(entry.firstFail) > loginFailureWindow {
		loginThrottleEntries.Delete(ip)
	}
	return true, 0
}

// loginThrottleFailure 记录一次登录失败；窗口内累计达到阈值则锁定并留痕。
func loginThrottleFailure(ip string) {
	value, _ := loginThrottleEntries.LoadOrStore(ip, &loginThrottleEntry{})
	entry := value.(*loginThrottleEntry)
	now := time.Now()
	if entry.failures == 0 || now.Sub(entry.firstFail) > loginFailureWindow {
		entry.firstFail = now
		entry.failures = 1
		return
	}
	entry.failures++
	if entry.failures >= loginMaxFailures {
		entry.lockedUntil = now.Add(loginLockoutDuration)
		log.Warnf("SECURITY AUDIT: login locked for IP %s after %d failed attempts, unlock at %s",
			ip, entry.failures, entry.lockedUntil.Format(time.RFC3339))
	}
}

// loginThrottleSuccess 登录成功后清除该 IP 的失败记录。
func loginThrottleSuccess(ip string) {
	loginThrottleEntries.Delete(ip)
}
