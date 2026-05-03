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

# Create a non-root user that mirrors the Colima VM default user.
# All bootstrap scripts run as this user with passwordless sudo.
# Ubuntu 24.04 ships with an `ubuntu` user at UID 1000; remove it first.
RUN userdel -r ubuntu 2>/dev/null || true && \
    useradd -m -u 1000 -s /bin/bash user && \
    echo "user ALL=(ALL) NOPASSWD: ALL" >> /etc/sudoers

USER user
ENV HOME=/home/user
WORKDIR /home/user

CMD ["sleep", "infinity"]
