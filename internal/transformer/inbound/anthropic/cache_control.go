package anthropic

import (
	"github.com/bestruirui/octopus/internal/transformer/model"
)

// convertToLLMCacheControl converts an Anthropic wire cache_control into the internal model.
func convertToLLMCacheControl(c *CacheControl) *model.CacheControl {
	if c == nil {
		return nil
	}

	return &model.CacheControl{
		Type: c.Type,
		TTL:  c.TTL,
	}
}
