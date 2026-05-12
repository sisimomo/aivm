FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Install the minimum tooling bootstrap scripts expect to find.
# sudo is installed so that scripts using "sudo <cmd>" work correctly.
# The container already provides curl and bash used by plugin install scripts.
RUN apt-get update -qq && \
    apt-get install -y --no-install-recommends \
      sudo \
      bash \
      curl \
      ca-certificates \
      lsb-release \
      python3 \
      jq \
      git \
      unzip \
      zip \
      wget \
      gnupg \
      libatomic1 \
      build-essential && \
    rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash user && \
    echo "user ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/user && \
    chmod 0440 /etc/sudoers.d/user

USER user
ENV HOME=/home/user
WORKDIR /home/user

# Pre-install mise so the mise plugin skip_if check passes in tests.
# The skip_if for the mise plugin checks: mise --version && grep -q 'mise activate' ~/.bashrc
RUN curl -fsSL https://mise.run | sh && \
    echo 'eval "$($HOME/.local/bin/mise activate bash)"' >> ~/.bashrc

# Make mise available for subsequent RUN steps.
ENV PATH="/home/user/.local/bin:${PATH}"

# Pre-install node@latest via mise so the mise-node plugin is already satisfied.
# claude and t3code both declare a mise-node dependency; having node pre-installed
# avoids a slow download on every test run.
RUN mise use --global node@latest && \
    mise reshim

CMD ["sleep", "infinity"]
