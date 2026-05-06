package copilot

import (
	"context"
	"fmt"
	"strings"

	copilot "github.com/github/copilot-sdk/go"
)

// Review sends a code diff as a prompt to Copilot and returns the review response
// Uses SDK session for lifecycle management and event-driven response collection
func (rc *ReviewClient) Review(ctx context.Context, diff string, model string) (string, error) {
	if rc.client == nil {
		return "", fmt.Errorf("client not initialized")
	}

	// Create session with specified model
	session, err := rc.client.CreateSession(ctx, &copilot.SessionConfig{
		Model:               model,
		OnPermissionRequest: copilot.PermissionHandler.ApproveAll,
	})
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	defer session.Disconnect()

	// Collect review response via event handler
	var reviewText strings.Builder
	done := make(chan bool)

	session.On(func(event copilot.SessionEvent) {
		switch d := event.Data.(type) {
		case *copilot.AssistantMessageData:
			reviewText.WriteString(d.Content)
		case *copilot.SessionIdleData:
			close(done)
		}
	})

	// Format diff as review prompt
	prompt := fmt.Sprintf("Review this code diff and provide feedback:\n\n%s", diff)

	// Send prompt to Copilot
	_, err = session.Send(ctx, copilot.MessageOptions{
		Prompt: prompt,
	})
	if err != nil {
		return "", fmt.Errorf("send prompt: %w", err)
	}

	// Wait for response completion (or context timeout)
	select {
	case <-done:
		// Response complete
	case <-ctx.Done():
		return "", ctx.Err()
	}

	result := strings.TrimSpace(reviewText.String())
	if result == "" {
		return "", fmt.Errorf("empty response from copilot")
	}

	return result, nil
}
