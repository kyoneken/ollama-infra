#!/usr/bin/env bash
set -euo pipefail

REVIEW_OUTPUT="${REVIEW_OUTPUT:-/tmp/review.txt}"
DIFF_FILE="/tmp/pr.diff"
# Use COPILOT_MODEL env var if set; fall back to default
MODEL="${COPILOT_MODEL:-qwen2.5-coder:1.5b}"

log() { echo "[entrypoint] $*"; }

# --- Start Ollama ---
log "Starting ollama serve..."
ollama serve &
OLLAMA_PID=$!

# --- Wait for Ollama to be ready ---
log "Waiting for Ollama to be ready (timeout 60s)..."
READY=false
for i in $(seq 1 60); do
  if curl -sf http://localhost:11434 > /dev/null 2>&1; then
    READY=true
    break
  fi
  sleep 1
done

if [[ "$READY" != "true" ]]; then
  log "ERROR: Ollama did not become ready within 60 seconds."
  exit 1
fi
log "Ollama is ready."

# --- Pull model ---
log "Pulling model: ${MODEL} ..."
ollama pull "${MODEL}"

# --- Resolve diff input ---
if [[ -n "${PR_DIFF:-}" ]]; then
  log "Writing PR_DIFF env var to ${DIFF_FILE}..."
  printf '%s' "$PR_DIFF" > "$DIFF_FILE"
elif [[ -f /workspace/pr.diff ]]; then
  log "Using /workspace/pr.diff..."
  cp /workspace/pr.diff "$DIFF_FILE"
else
  log "ERROR: No diff found. Set PR_DIFF env var or provide /workspace/pr.diff."
  exit 1
fi

# --- Authenticate gh / copilot if token provided ---
if [[ -n "${GITHUB_TOKEN:-}" ]]; then
  log "GITHUB_TOKEN detected; configuring gh auth..."
  echo "$GITHUB_TOKEN" | gh auth login --with-token 2>/dev/null || true
fi

# --- Truncate diff to prevent context overflow ---
# Limit to ~8000 chars (~2000 tokens) to stay well within model context window
MAX_DIFF_CHARS=8000
DIFF_CONTENT=$(head -c "${MAX_DIFF_CHARS}" "${DIFF_FILE}")
DIFF_LEN=$(wc -c < "${DIFF_FILE}")
if [[ "${DIFF_LEN}" -gt "${MAX_DIFF_CHARS}" ]]; then
  DIFF_CONTENT="${DIFF_CONTENT}
[... diff truncated at ${MAX_DIFF_CHARS} chars — only showing start of diff ...]"
  log "Diff truncated from ${DIFF_LEN} chars to ${MAX_DIFF_CHARS} chars."
fi

# --- Run Copilot code review ---
PROMPT="You are a code reviewer. Review the following diff and report: typos in identifiers/strings/comments, simple logic errors (off-by-one, null checks, missing returns), and discrepancies between comments and code. For each issue output: FILE, LINE, SEVERITY, DESCRIPTION, SUGGESTION.

Diff:
${DIFF_CONTENT}"

log "Running Copilot code review..."
# Non-interactive flags:
#   --allow-all-tools : required for -p non-interactive mode
#   --no-ask-user     : autonomous mode
#   --no-remote       : disable GitHub remote session tracking
#   --disable-builtin-mcps : skip GitHub MCP API calls
#   --excluded-tools shell : prevent multi-round shell execution that causes timeout
#   -s                : silent (response only, no stats)
#   timeout 240       : hard limit 4 min (under Copilot CLI's 5-min HTTP timeout)
REVIEW_TEXT=$(timeout 240 copilot \
  -p "$PROMPT" \
  --agent code-reviewer \
  --allow-all-tools \
  --allow-all-paths \
  --no-ask-user \
  --no-remote \
  --disable-builtin-mcps \
  --excluded-tools "shell" \
  -s \
  2>&1) || {
  log "WARNING: copilot exited with non-zero status; capturing output anyway."
  REVIEW_TEXT="${REVIEW_TEXT:-copilot failed or timed out}"
}

# --- Write output ---
printf '%s\n' "$REVIEW_TEXT" > "$REVIEW_OUTPUT"
log "Review written to ${REVIEW_OUTPUT}"

echo ""
echo "========== CODE REVIEW =========="
echo "$REVIEW_TEXT"
echo "================================="
