FROM mcr.microsoft.com/devcontainers/go:2-1.25-trixie

# Install Docker CE CLI + Compose plugin directly from Docker's apt repo.
# This avoids the devcontainer docker-outside-of-docker feature, whose install
# script pulls in the Yarn apt repo (expired GPG key as of early 2026).
# The base image ships with a Yarn apt source whose GPG key has expired.
# Remove it before running apt-get update to avoid the NO_PUBKEY error.
RUN rm -f /etc/apt/sources.list.d/yarn.list

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        apt-transport-https \
        ca-certificates \
        curl \
        gnupg \
        lsb-release \
    && install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/debian/gpg \
        | gpg --dearmor -o /etc/apt/keyrings/docker.gpg \
    && chmod a+r /etc/apt/keyrings/docker.gpg \
    && echo \
        "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] \
        https://download.docker.com/linux/debian \
        $(lsb_release -cs) stable" \
        > /etc/apt/sources.list.d/docker.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        docker-ce-cli \
        docker-compose-plugin \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*
