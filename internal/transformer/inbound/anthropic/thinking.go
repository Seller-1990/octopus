package anthropic

// thinkingBudgetToReasoningEffort converts thinking budget tokens to reasoning effort string.
func thinkingBudgetToReasoningEffort(budgetTokens int64) string {
	// Map budget tokens to reasoning effort based on the same logic used in outbound
	if budgetTokens <= 5000 {
		return EffortLow
	} else if budgetTokens <= 15000 {
		return EffortMedium
	} else {
		return EffortHigh
	}
}
