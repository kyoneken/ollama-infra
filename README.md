# ollama-infra

## Overview

Automated AI code review on pull requests using a fully local LLM stack — no external AI API keys required.

```
PR opened/updated
      │
      ▼
GitHub Actions (ubuntu-latest)
      │
      ▼
Docker build (Ubuntu 22.04 + gh + Node.js + Copilot CLI + Ollama)
      │
      ▼
ollama serve  ──►  pull qwen2.5-coder:7b
      │
      ▼
Copilot CLI  ──►  code-reviewer agent
      │                 ├─ typo-checker
      │                 ├─ logic-reviewer
      │                 └─ comment-consistency
      │
      ▼
Review output  ──►  PR comment (via github-script)
```

## How It Works

1. **Trigger**: Any PR `opened`, `synchronize`, or `reopened` event fires the `ai-review` workflow.
2. **Diff generation**: `git diff origin/<base>...HEAD` is saved to `pr.diff`.
3. **Docker build**: The image bundles Ubuntu 22.04, GitHub CLI, Node.js 20, `@github/copilot-cli`, and Ollama. Docker layer caching keeps rebuilds fast.
4. **BYOK (Bring Your Own Key)**: The image is pre-configured with environment variables that point Copilot CLI at the local Ollama instance instead of GitHub's servers:
   - `COPILOT_PROVIDER_BASE_URL=http://localhost:11434`
   - `COPILOT_MODEL=qwen2.5-coder:7b`
   - `COPILOT_OFFLINE=true`
5. **Review**: `entrypoint.sh` starts Ollama, pulls the model, feeds the diff to Copilot CLI, and writes the output to `review_output.txt`.
6. **Comment**: A `github-script` step reads the file and posts it as a PR comment.

## Skills

| Skill | What it checks |
|---|---|
| **typo-checker** | Spelling mistakes in identifiers, string literals, comments, and parameter names. Conservative — only flags high-confidence typos. |
| **logic-reviewer** | Off-by-one errors, missing null/nil checks, control flow bugs (unreachable code, missing returns), operator mistakes, and unchecked errors. |
| **comment-consistency** | Mismatches between comments/docstrings and actual code behavior, outdated TODOs, stale examples, and misleading names. |

## Custom Agent

The `code-reviewer` agent (`.copilot/agents/code-reviewer.md`) orchestrates all three skills. It reviews every changed file line-by-line and outputs structured findings:

```
FILE: <path>
LINE: <number>
SEVERITY: <typo | logic | comment>
DESCRIPTION: <problem>
SUGGESTION: <fix>

SUMMARY: X issue(s) found across Y file(s).
```

Only real correctness issues are reported — no style preferences or refactor suggestions.

## Configuration

| Secret / Variable | Source | Purpose |
|---|---|---|
| `GITHUB_TOKEN` | Automatic (Actions) | Post PR comments |
| `COPILOT_PROVIDER_BASE_URL` | Baked into image | Points Copilot CLI → Ollama |
| `COPILOT_MODEL` | Baked into image | Model name to use |
| `COPILOT_OFFLINE` | Baked into image | Skips GitHub auth check |

No additional secrets are needed. Everything runs offline inside the container.

## Local Development / Testing

```bash
# Build the image
docker build -t ollama-review .

# Generate a diff (adjust branch name as needed)
git diff main...HEAD > pr.diff

# Run the review (output written to review.txt in current directory)
docker run --rm \
  -v $(pwd):/workspace \
  -e REVIEW_OUTPUT=/workspace/review.txt \
  ollama-review

cat review.txt
```

Pass the diff via environment variable instead of a file:

```bash
docker run --rm \
  -e PR_DIFF="$(git diff main...HEAD)" \
  -e REVIEW_OUTPUT=/workspace/review.txt \
  -v $(pwd):/workspace \
  ollama-review
```

GPU acceleration (faster inference):

```bash
docker run --rm --gpus all \
  -v $(pwd):/workspace \
  -e REVIEW_OUTPUT=/workspace/review.txt \
  ollama-review
```

## Adding / Modifying Skills

1. Create `.copilot/skills/your-skill.md` following the same front-matter format:
   ```markdown
   ---
   name: your-skill
   description: One-line description
   ---
   Instructions for the LLM...
   ```
2. Reference it in `.copilot/agents/code-reviewer.md` under `skills:`.
3. Rebuild the Docker image — skills are baked in at build time.

## Notes

- **First run**: downloads `qwen2.5-coder:7b` (~4.7 GB) inside the container. Subsequent runs reuse Docker layer cache, so this only happens when the `Dockerfile` or `entrypoint.sh` changes.
- **Fully offline**: `COPILOT_OFFLINE=true` means no GitHub authentication is required for the LLM calls.
- **Timeout**: the workflow has a 30-minute timeout to accommodate the model download on cold starts.
- **Non-blocking**: the review step uses `continue-on-error: true` so a failed review never blocks a merge.
