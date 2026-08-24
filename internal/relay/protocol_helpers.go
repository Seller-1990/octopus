package relay

import (
	"context"
	"fmt"
	"strings"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/relay/balancer"
	"github.com/bestruirui/octopus/internal/transformer/inbound"
	transformerModel "github.com/bestruirui/octopus/internal/transformer/model"
	"github.com/gin-gonic/gin"
)

func clientSuccessTerminalEvents(format transformerModel.APIFormat) map[string]struct{} {
	switch format {
	case transformerModel.APIFormatOpenAIResponse:
		return map[string]struct{}{
			"response.completed":  {},
			"response.failed":     {},
			"response.incomplete": {},
			"response.cancelled":  {},
			"response.canceled":   {},
		}
	case transformerModel.APIFormatAnthropicMessage:
		return map[string]struct{}{"message_stop": {}, "error": {}}
	case transformerModel.APIFormatOpenAIChatCompletion:
		return map[string]struct{}{"[DONE]": {}}
	default:
		return nil
	}
}

func requestOutcomeForTerminalEvent(event string) dbmodel.RequestOutcome {
	switch strings.TrimSpace(event) {
	case "response.incomplete":
		return dbmodel.RequestOutcomeIndeterminate
	case "response.cancelled", "response.canceled":
		return dbmodel.RequestOutcomeIndeterminate
	case "response.failed", "response.error", "error":
		return dbmodel.RequestOutcomeFailed
	default:
		return dbmodel.RequestOutcomeSuccess
	}
}

func inboundProtocolName(value inbound.InboundType) dbmodel.ProtocolName {
	switch value {
	case inbound.InboundTypeOpenAIResponse:
		return dbmodel.ProtocolOpenAIResponses
	case inbound.InboundTypeAnthropic:
		return dbmodel.ProtocolAnthropic
	case inbound.InboundTypeOpenAIEmbedding:
		return dbmodel.ProtocolOpenAIEmbedding
	default:
		return dbmodel.ProtocolOpenAIChat
	}
}

func routeDecisionMap(preview dbmodel.RoutePreview) map[string]dbmodel.RouteDecisionReason {
	decisions := make(map[string]dbmodel.RouteDecisionReason, len(preview.Decisions))
	for _, decision := range preview.Decisions {
		if !decision.Included {
			continue
		}
		decisions[relayRouteDecisionKey(decision.ChannelID, decision.UpstreamModel)] = decision
	}
	return decisions
}

func recordProtocolPlanningSkips(
	ctx context.Context,
	iter *balancer.Iterator,
	preview dbmodel.RoutePreview,
) {
	if iter == nil {
		return
	}
	for _, decision := range preview.Decisions {
		if decision.Included {
			continue
		}
		channelName := fmt.Sprintf("channel_%d", decision.ChannelID)
		if channel, err := op.ChannelGet(decision.ChannelID, ctx); err == nil {
			channelName = channel.Name
		}
		reason := strings.TrimSpace(decision.Reason)
		if reason == "" {
			reason = "excluded by protocol planning"
		}
		iter.RecordPlanningSkip(
			decision.ChannelID,
			channelName,
			decision.UpstreamModel,
			decision.RouteCandidateID,
			reason,
		)
	}
}

func relayRouteDecisionKey(channelID int, modelName string) string {
	return fmt.Sprintf("%d\x00%s", channelID, modelName)
}

func setProtocolWarningHeader(c *gin.Context, warnings []string) {
	if c == nil {
		return
	}
	c.Writer.Header().Del("X-Octopus-Warning")
	if len(warnings) > 0 {
		c.Header("X-Octopus-Warning", strings.Join(warnings, "; "))
	}
}
