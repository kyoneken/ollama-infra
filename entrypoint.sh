#!/usr/bin/env bash
set -euo pipefail

REVIEW_OUTPUT="${REVIEW_OUTPUT:-/tmp/review.txt}"
DIFF_FILE="/tmp/pr.diff"
# Use COPILOT_MODEL env var if set; fall back to default
BASE_MODEL="${COPILOT_MODEL:-qwen2.5-coder:1.5b}"
REVIEW_MODEL="qwen-reviewer"

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
log "Pulling model: ${BASE_MODEL} ..."
ollama pull "${BASE_MODEL}"

# --- Create a context-limited model to speed up CPU inference ---
# Copilot CLI's built-in system prompt alone exceeds 10,000 tokens, making it
# incompatible with small context windows. We call the Ollama API directly to
# keep the total prompt small (~400 tokens) and inference fast (~30s on CPU).
log "Creating context-limited model '${REVIEW_MODEL}' (num_ctx 4096)..."
cat > /tmp/Modelfile <<EOF
FROM ${BASE_MODEL}
PARAMETER num_ctx 4096
EOF
ollama create "${REVIEW_MODEL}" -f /tmp/Modelfile

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

# --- Truncate diff ---
# Keep under ~2000 chars (~500 tokens) so total prompt stays well within 4096
MAX_DIFF_CHARS=2000
DIFF_CONTENT=$(head -c "${MAX_DIFF_CHARS}" "${DIFF_FILE}")
DIFF_LEN=$(wc -c < "${DIFF_FILE}")
if [[ "${DIFF_LEN}" -gt "${MAX_DIFF_CHARS}" ]]; then
  DIFF_CONTENT="${DIFF_CONTENT}
[... diff truncated at ${MAX_DIFF_CHARS} chars ...]"
  log "Diff truncated from ${DIFF_LEN} chars to ${MAX_DIFF_CHARS} chars."
fi

# --- Run code review via Ollama API directly ---
# Copilot CLI's built-in system prompt is ~10,000+ tokens which exceeds the
# model context window, making it unusable for CPU inference in CI.
# We call Ollama's OpenAI-compatible API directly to control prompt size.
# --- Run code review via 'ollama run' streaming to file ---
# The Ollama REST API waits for full completion before returning, which
# takes 5+ minutes on CPU runners. 'ollama run' streams tokens to stdout
# as they are generated, so redirecting to a file captures partial output
# even when killed by timeout.
log "Running code review via 'ollama run' (timeout 120s)..."

SYSTEM_PROMPT="You are a code reviewer. Report issues in this diff:
1. Typos in identifiers, strings, or comments
2. Logic errors (off-by-one, missing null checks, missing returns)
3. Comment-code discrepancies
Format each issue as: FILE | LINE | SEVERITY | DESCRIPTION | SUGGESTION
Output 'No issues found.' if the diff is clean."

cat > /tmp/review_prompt.txt <<PROMPT
${SYSTEM_PROMPT}

Review this diff:

${DIFF_CONTENT}
PROMPT

timeout 120 ollama run "${REVIEW_MODEL}" --nowordwrap \
  < /tmp/review_prompt.txt \
  > /tmp/review_partial.txt 2>/dev/null || {
  log "ollama run timed out or exited non-zero; using partial output."
}

REVIEW_TEXT=$(cat /tmp/review_partial.txt 2>/dev/null || echo "")
if [[ -z "$REVIEW_TEXT" ]]; then
  REVIEW_TEXT="No review output generated (model may be too slow for CPU inference)."
fi

# --- Write output ---
printf '%s\n' "$REVIEW_TEXT" > "$REVIEW_OUTPUT"
log "Review written to ${REVIEW_OUTPUT}"

echo ""
echo "========== CODE REVIEW =========="
echo "$REVIEW_TEXT"
echo "================================="
