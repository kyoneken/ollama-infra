package copilot_test

import (
	"testing"

	// Test that the Copilot SDK can be imported
	_ "github.com/github/copilot-sdk/go"
)

func TestCopilotSDKImport(t *testing.T) {
	// This test verifies that the Copilot SDK dependency is properly installed.
	// If this test passes, the SDK is available for use in the project.
	t.Log("Copilot SDK is successfully imported")
}
