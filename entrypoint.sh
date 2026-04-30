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

# --- Pull model (no-op if already baked into Docker image) ---
log "Verifying model ${BASE_MODEL} is present..."
ollama pull "${BASE_MODEL}" || log "Pull skipped or failed — model should already be in image."

# --- Create a context-limited model to speed up CPU inference ---
# Copilot CLI's built-in system prompt alone exceeds 10,000 tokens, making it
# incompatible with small context windows. We call the Ollama API directly to
# keep the total prompt small (~400 tokens) and inference fast (~30s on CPU).
log "Creating context-limited model '${REVIEW_MODEL}' (num_ctx 512)..."
cat > /tmp/Modelfile <<EOF
FROM ${BASE_MODEL}
PARAMETER num_ctx 512
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
# ~800 chars ≈ 200 tokens. With ~60 token system prompt = ~260 token prefill.
# At ~0.8 tok/s on 2 vCPU: 260s prefill + 200s generation + 30s load = ~490s.
# Use 480s timeout for ~90s safety margin.
MAX_DIFF_CHARS=800
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
# stream:true — Ollama sends each token as it's generated via NDJSON; we
# capture partial output even if the curl timeout fires before generation ends.
log "Running code review (stream:true, num_predict:200, timeout 480s)..."

SYSTEM_PROMPT="Review this diff. For each bug, typo, or comment mismatch output:
FILE|LINE|SEVERITY|ISSUE|FIX
One line per issue. Be concise."

FULL_PROMPT="${SYSTEM_PROMPT}

${DIFF_CONTENT}"

# Build JSON payload — use Python to properly escape prompt content
python3 -c "
import json, sys
payload = {
    'model': sys.argv[1],
    'prompt': sys.argv[2],
    'stream': True,
    'options': {'num_predict': 200}
}
with open('/tmp/review_payload.json', 'w') as f:
    json.dump(payload, f)
" "${REVIEW_MODEL}" "${FULL_PROMPT}"

log "Payload written."

# --- Main review request: stream:true ---
# Ollama sends NDJSON: {"response":"token","done":false}\n per token.
# -N disables curl's output buffering so each chunk reaches the file immediately.
log "Starting review (stream:true, num_predict:200, timeout 480s)..."
CURL_EXIT=0
curl -s -N -m 480 \
  -X POST http://localhost:11434/api/generate \
  -H 'Content-Type: application/json' \
  --data @/tmp/review_payload.json \
  > /tmp/raw_stream.ndjson 2>/tmp/curl_err.txt || CURL_EXIT=$?

log "Review curl exit: ${CURL_EXIT}, size: $(wc -c < /tmp/raw_stream.ndjson) bytes"
[[ -s /tmp/curl_err.txt ]] && log "curl stderr: $(cat /tmp/curl_err.txt)"

# Extract .response tokens from the NDJSON stream
python3 - << 'PYEOF' > /tmp/review_partial.txt
import json, sys
try:
    with open('/tmp/raw_stream.ndjson', 'rb') as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                d = json.loads(line)
                t = d.get('response', '')
                if t:
                    sys.stdout.write(t)
            except json.JSONDecodeError:
                pass
except Exception as e:
    sys.stderr.write(f'[parse] error: {e}\n')
PYEOF

log "Streaming done. Output: $(wc -c < /tmp/review_partial.txt 2>/dev/null || echo 0) bytes"

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
