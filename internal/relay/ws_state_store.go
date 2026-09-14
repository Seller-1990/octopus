package relay

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/bestruirui/octopus/internal/relay/balancer"
)

type wsConversationStateEntry struct {
	state     *wsConversationState
	expiresAt time.Time
}

var wsConversationStore sync.Map // key: apiKeyID:requestModel:downstreamSessionID -> *wsConversationStateEntry

// 惰性清理若每次写入都做，就是对全表的 Range 扫描（与 F04 修复的
// ws_runtime_state 同类问题）。按时间节流后均摊为常数；过期条目最晚在
// 下一个节流窗口被清掉，不影响 TTL 语义（单键读取路径本就有过期检查）。
const wsConversationPruneInterval = time.Minute

var wsConversationLastPrune atomic.Int64

func wsConversationStateKey(apiKeyID int, requestModel, downstreamSessionID string) string {
	return fmt.Sprintf("%d:%s:%s", apiKeyID, strings.TrimSpace(requestModel), strings.TrimSpace(downstreamSessionID))
}

func loadWSConversationState(apiKeyID int, requestModel, downstreamSessionID string) *wsConversationState {
	requestModel = strings.TrimSpace(requestModel)
	downstreamSessionID = strings.TrimSpace(downstreamSessionID)
	if requestModel == "" || downstreamSessionID == "" {
		return nil
	}

	v, ok := wsConversationStore.Load(wsConversationStateKey(apiKeyID, requestModel, downstreamSessionID))
	if !ok {
		return nil
	}

	entry, ok := v.(*wsConversationStateEntry)
	if !ok || entry == nil || entry.state == nil {
		wsConversationStore.Delete(wsConversationStateKey(apiKeyID, requestModel, downstreamSessionID))
		return nil
	}
	if !entry.expiresAt.IsZero() && time.Now().After(entry.expiresAt) {
		wsConversationStore.Delete(wsConversationStateKey(apiKeyID, requestModel, downstreamSessionID))
		return nil
	}

	return cloneWSConversationState(entry.state)
}

func storeWSConversationState(apiKeyID int, requestModel string, state *wsConversationState, ttl time.Duration) {
	requestModel = strings.TrimSpace(requestModel)
	downstreamSessionID := ""
	if state != nil {
		downstreamSessionID = strings.TrimSpace(state.DownstreamSessionID)
	}
	if requestModel == "" || state == nil || downstreamSessionID == "" {
		return
	}
	if ttl <= 0 {
		ttl = wsClientMaxAge
	}

	cloned := cloneWSConversationState(state)
	if cloned == nil {
		return
	}
	cloned.RequestModel = requestModel

	if nowUnix := time.Now().Unix(); nowUnix-wsConversationLastPrune.Load() >= int64(wsConversationPruneInterval/time.Second) {
		// 先占位再清理：并发下至多出现少量重复清理，无害；Range 删除幂等。
		wsConversationLastPrune.Store(nowUnix)
		pruneExpiredWSConversationStates(time.Now())
	}
	wsConversationStore.Store(wsConversationStateKey(apiKeyID, requestModel, downstreamSessionID), &wsConversationStateEntry{
		state:     cloned,
		expiresAt: time.Now().Add(ttl),
	})
}

func deleteWSConversationState(apiKeyID int, requestModel, downstreamSessionID string) {
	requestModel = strings.TrimSpace(requestModel)
	downstreamSessionID = strings.TrimSpace(downstreamSessionID)
	if requestModel == "" || downstreamSessionID == "" {
		return
	}
	wsConversationStore.Delete(wsConversationStateKey(apiKeyID, requestModel, downstreamSessionID))
}

func deleteWSConversationStatesForSession(apiKeyID int, downstreamSessionID string) {
	downstreamSessionID = strings.TrimSpace(downstreamSessionID)
	if downstreamSessionID == "" {
		return
	}
	wsConversationStore.Range(func(key, value any) bool {
		entry, ok := value.(*wsConversationStateEntry)
		if !ok || entry == nil || entry.state == nil {
			wsConversationStore.Delete(key)
			return true
		}
		if entry.state.DownstreamSessionID == downstreamSessionID &&
			strings.HasPrefix(fmt.Sprint(key), fmt.Sprintf("%d:", apiKeyID)) {
			wsConversationStore.Delete(key)
		}
		return true
	})
}

func pruneExpiredWSConversationStates(now time.Time) {
	wsConversationStore.Range(func(key, value any) bool {
		entry, ok := value.(*wsConversationStateEntry)
		if !ok || entry == nil || entry.state == nil ||
			(!entry.expiresAt.IsZero() && !now.Before(entry.expiresAt)) {
			wsConversationStore.Delete(key)
		}
		return true
	})
}

func resolveWSConversationState(apiKeyID int, requestModel string, localState *wsConversationState, allowStoredRestore bool, downstreamSessionID string) *wsConversationState {
	requestModel = strings.TrimSpace(requestModel)
	downstreamSessionID = strings.TrimSpace(downstreamSessionID)
	if requestModel == "" {
		return localState
	}
	if localState != nil && localState.MatchesRequestModel(requestModel) {
		return localState
	}
	if !allowStoredRestore {
		return nil
	}
	return loadWSConversationState(apiKeyID, requestModel, downstreamSessionID)
}

func wsConversationStateToSticky(state *wsConversationState) *balancer.SessionEntry {
	if state == nil || state.ChannelID <= 0 {
		return nil
	}
	return &balancer.SessionEntry{
		ChannelID:    state.ChannelID,
		ChannelKeyID: state.ChannelKeyID,
		Timestamp:    time.Now(),
	}
}

func wsConversationStateTTL(sessionKeepTimeSec int) time.Duration {
	if sessionKeepTimeSec <= 0 {
		return wsClientMaxAge
	}
	ttl := time.Duration(sessionKeepTimeSec) * time.Second
	if ttl > wsClientMaxAge {
		return wsClientMaxAge
	}
	return ttl
}

func resetWSConversationStateStore() {
	wsConversationStore = sync.Map{}
}
