package copilot

import (
"context"
"fmt"
"os"
"path/filepath"
)

// DiagnoseSkills logs skill directories configuration and availability
func DiagnoseSkills(skillDirs []string) {
if len(skillDirs) == 0 {
fmt.Fprintf(os.Stderr, "[copilot-sdk] Skills: disabled (empty skillDirs)\n")
fmt.Fprintf(os.Stderr, "[copilot-sdk] Note: Skills can be configured via SkillDirectories in SessionConfig\n")
return
}

fmt.Fprintf(os.Stderr, "[copilot-sdk] Skills Configuration:\n")
for i, dir := range skillDirs {
absPath, _ := filepath.Abs(dir)
stat, err := os.Stat(dir)
if err != nil {
fmt.Fprintf(os.Stderr, "[copilot-sdk]   [%d] %s (NOT FOUND)\n", i+1, absPath)
} else {
fmt.Fprintf(os.Stderr, "[copilot-sdk]   [%d] %s (%s, %d bytes)\n", 
i+1, absPath, stat.Mode(), stat.Size())

// List contents if it's a directory
if stat.IsDir() {
entries, err := os.ReadDir(dir)
if err == nil && len(entries) > 0 {
fmt.Fprintf(os.Stderr, "[copilot-sdk]       Contents:\n")
for _, e := range entries {
fmt.Fprintf(os.Stderr, "[copilot-sdk]         - %s (%v)\n", 
e.Name(), e.IsDir())
}
}
}
}
}
}

// PromptForSkillDetection returns a prompt that encourages SDK/skills to report
// what skills they detect and use during review
func PromptForSkillDetection(diff string) string {
return fmt.Sprintf(`You are a code reviewer with access to various skills.
Before providing your review, briefly list any skills you're using to analyze this code.
Format your response as:
  SKILLS DETECTED: [skill1, skill2, ...]
  ANALYSIS: [your detailed review]

Code diff to review:
%s`, diff)
}
