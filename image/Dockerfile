FROM debian:bookworm-slim

ARG TARGETARCH
ARG GO_VERSION=1.26.6
ARG NODE_MAJOR=22

ENV DEBIAN_FRONTEND=noninteractive
ENV PATH=/usr/local/go/bin:$PATH

RUN apt-get update \
    && apt-get install --yes --no-install-recommends \
        bash \
        build-essential \
        ca-certificates \
        curl \
        git \
        gnupg \
        jq \
        less \
        openssh-client \
        procps \
        python3 \
        python3-pip \
        python3-venv \
        unzip \
        util-linux \
        xz-utils \
    && rm -rf /var/lib/apt/lists/*

RUN curl -fsSL "https://deb.nodesource.com/setup_${NODE_MAJOR}.x" | bash - \
    && apt-get install --yes --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/* \
    && corepack enable

RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" \
    | tar -C /usr/local -xz

RUN npm install --global @anthropic-ai/claude-code \
    && npm cache clean --force

CMD ["sleep", "infinity"]
