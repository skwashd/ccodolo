FROM public.ecr.aws/docker/library/debian:trixie-slim

ARG TZ
ENV TZ="$TZ"

ARG CLAUDE_CODE_VERSION=latest
ARG USERNAME=coder

LABEL org.opencontainers.image.description="Sandboxed Docker containers for AI coding assistants with isolated project environments"
LABEL org.opencontainers.image.licenses=MIT
LABEL org.opencontainers.image.source="https://github.com/skwashd/ccodolo"
LABEL org.opencontainers.image.title="CCoDoLo"
LABEL org.opencontainers.image.url="https://github.com/skwashd/ccodolo"
LABEL org.opencontainers.image.vendor="skwashd"

RUN apt update \
  && apt upgrade -y \
  && apt install -y --no-install-recommends \
    ca-certificates \
    curl \
    gnupg2 \
    lsb-release \
    wget \
  && wget -O- https://apt.releases.hashicorp.com/gpg | gpg --dearmor | tee /usr/share/keyrings/hashicorp-archive-keyring.gpg > /dev/null \
  && echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(grep -oP '(?<=UBUNTU_CODENAME=).*' /etc/os-release || lsb_release -cs) main" | tee /etc/apt/sources.list.d/hashicorp.list \
  && apt update \
  && apt install -y --no-install-recommends \
    aggregate \
    dnsutils \
    fzf \
    gh \
    git \
    git-delta \
    golang \
    iproute2 \
    jq \
    less \
    man-db \
    nano \
    nodejs \
    npm \
    procps \
    python3.13-full \
    python3-pip \
    sudo \
    terraform \
    unzip \
    vim \
    zsh \
  && apt-get clean && rm -rf /var/lib/apt/lists/* \
  && adduser $USERNAME --shell /bin/zsh --disabled-password \
  && mkdir /commandhistory \
  && touch /commandhistory/.bash_history /commandhistory/.zsh_history \
  && chown -R $USERNAME /commandhistory \
  && mkdir -p /workspace /home/$USERNAME/.claude /home/$USERNAME/.local/bin \
  && chown -R $USERNAME:$USERNAME /workspace /home/$USERNAME \
  && cd /tmp \
  && curl "https://awscli.amazonaws.com/awscli-exe-linux-aarch64.zip" -o "awscliv2.zip" \
  && unzip awscliv2.zip \
  && ./aws/install --bin-dir /usr/local/bin --install-dir /usr/local/aws-cli \
  && rm -rf /tmp/awscliv2* aws/


COPY dotfiles/* /home/$USERNAME/
COPY bin/start-agent /home/$USERNAME/.local/bin/start-agent

USER $USERNAME
WORKDIR /workspace

ENV SHELL=/bin/zsh

# Default powerline10k theme
ARG ZSH_IN_DOCKER_VERSION=1.2.0
RUN sh -c "$(wget -O- https://github.com/deluan/zsh-in-docker/releases/download/v${ZSH_IN_DOCKER_VERSION}/zsh-in-docker.sh)" -- \
    -p git -p fzf -x \
  && curl -LsSf https://astral.sh/uv/install.sh | sh \
  && curl -fsSL https://claude.ai/install.sh | bash \
  && npm config set prefix '~/.local/' \
  && npm config set cache '~/.npm' \
  && npm config set global true \
  && npm install -g @github/copilot \
    @google/gemini-cli \
    @openai/codex \
    @playwright/cli@latest \
    aws-cdk \
    opencode-ai


ENV EDITOR=vim
ENV HISTFILE=/commandhistory/.zsh_history
ENV HISTSIZE=100000
ENV PATH="/home/coder/.local/bin:$PATH"
ENV SAVEHIST=100000
ENV VISUAL=vim

ENTRYPOINT [ "/home/coder/.local/bin/start-agent" ]