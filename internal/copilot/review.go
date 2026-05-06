package copilot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	copilot "github.com/github/copilot-sdk/go"
)

// Review sends a code diff as a prompt to Copilot and returns the review response
// Uses SDK session for lifecycle management and event-driven response collection
// Loads skills from SkillDirectories to delegate review logic to skills
func (rc *ReviewClient) Review(ctx context.Context, diff string, model string, skillDirs []string) (string, error) {
	if rc.client == nil {
		return "", fmt.Errorf("client not initialized")
	}

	// Log skill discovery status
	if len(skillDirs) > 0 {
		fmt.Fprintf(os.Stderr, "[copilot-sdk] SkillDirectories configured: %d directories\n", len(skillDirs))
		for i, dir := range skillDirs {
			absPath, _ := filepath.Abs(dir)
			exists := "missing"
			if _, err := os.Stat(dir); err == nil {
				exists = "found"
			}
			fmt.Fprintf(os.Stderr, "[copilot-sdk]   [%d] %s (%s)\n", i+1, absPath, exists)
		}
	} else {
		fmt.Fprintf(os.Stderr, "[copilot-sdk] SkillDirectories: disabled (config discovery only)\n")
	}

	// Create session with Skills enabled
	// EnableConfigDiscovery loads .mcp.json, .vscode/mcp.json, and skill directories from cwd
	// SkillDirectories specifies additional skill directories to load
	// Use empty model string to use SDK's default model (works with both GitHub and Ollama)
	session, err := rc.client.CreateSession(ctx, &copilot.SessionConfig{
		Model:                   "", // Use default model for better compatibility
		SkillDirectories:        skillDirs,
		EnableConfigDiscovery:   true,
		OnPermissionRequest:     copilot.PermissionHandler.ApproveAll,
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

	// Minimal prompt - delegate to skills for actual review logic
	prompt := fmt.Sprintf("Review this code diff:\n\n%s", diff)

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
