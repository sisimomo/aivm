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
      lsb-release && \
    rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash user && \
    echo "user ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/user && \
    chmod 0440 /etc/sudoers.d/user

USER user
ENV HOME=/home/user
WORKDIR /home/user

CMD ["sleep", "infinity"]
