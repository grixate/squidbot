package contextctrl

import (
	"encoding/json"
	"math"

	"github.com/grixate/squidbot/internal/provider"
)

func normalizeCharsPerToken(value float64) float64 {
	if value <= 0 {
		return defaultCharsPerToken
	}
	return value
}

func EstimateMessagesTokens(messages []provider.Message, charsPerToken float64) int {
	charsPerToken = normalizeCharsPerToken(charsPerToken)
	if len(messages) == 0 {
		return 0
	}
	totalChars := 0
	for _, msg := range messages {
		totalChars += estimateMessageChars(msg)
	}
	estimated := int(math.Ceil(float64(totalChars) / charsPerToken))
	estimated += len(messages)*6 + 8
	if estimated < 0 {
		return 0
	}
	return estimated
}

func estimateMessageChars(msg provider.Message) int {
	total := len(msg.Role) + len(msg.Content) + len(msg.Name) + len(msg.ToolCallID) + 12
	for _, tc := range msg.ToolCalls {
		total += len(tc.ID) + len(tc.Name)
		if len(tc.Arguments) > 0 {
			total += len(tc.Arguments)
			continue
		}
		fallback, _ := json.Marshal(tc.Arguments)
		total += len(fallback)
	}
	return total
}
