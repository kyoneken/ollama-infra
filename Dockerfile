FROM ubuntu:22.04

ENV DEBIAN_FRONTEND=noninteractive

# Install base dependencies + GitHub CLI + Node.js 20 in a single layer
RUN apt-get update && apt-get install -y \
    curl \
    git \
    jq \
    zstd \
    ca-certificates \
    gnupg \
    lsb-release \
    && curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg \
    | dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg \
    && chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" \
    | tee /etc/apt/sources.list.d/github-cli.list > /dev/null \
    && apt-get update && apt-get install -y gh \
    && curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y nodejs \
    && rm -rf /var/lib/apt/lists/*

# Install Ollama binary only (CPU-only, no CUDA/ROCm — keeps image small for CI)
# The release format changed to .tar.zst; extract only the binary
RUN OLLAMA_VERSION=$(curl -s https://api.github.com/repos/ollama/ollama/releases/latest \
    | jq -r '.tag_name') \
    && curl -fsSL "https://github.com/ollama/ollama/releases/download/${OLLAMA_VERSION}/ollama-linux-amd64.tar.zst" \
    | zstd -d | tar -x -C /tmp bin/ollama \
    && install -m 755 /tmp/bin/ollama /usr/local/bin/ollama \
    && rm -rf /tmp/bin

# Install GitHub Copilot CLI
RUN npm install -g @github/copilot

# Environment variables for BYOK (Ollama local)
ENV COPILOT_PROVIDER_BASE_URL=http://localhost:11434
ENV COPILOT_MODEL=qwen2.5-coder:7b
ENV COPILOT_OFFLINE=true
ENV OLLAMA_HOST=0.0.0.0

WORKDIR /workspace

# Copy Copilot skills and agents
COPY .copilot/skills/ /root/.copilot/skills/
COPY .copilot/agents/ /root/.copilot/agents/

# Copy and configure entrypoint
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
