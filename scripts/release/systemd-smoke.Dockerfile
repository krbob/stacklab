ARG BASE_IMAGE=debian:bookworm-slim
FROM ${BASE_IMAGE}

ENV container=docker
ENV DEBIAN_FRONTEND=noninteractive

# The slim image excludes /usr/share/doc except copyright files. Remove only
# that broad exclusion so this fixture behaves like a normal Debian install.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
      adduser \
      ca-certificates \
      curl \
      docker-compose \
      docker.io \
      git \
      procps \
      systemd \
      systemd-sysv \
    && mkdir -p /etc/docker \
    && systemctl mask \
      console-getty.service \
      docker.service \
      docker.socket \
      getty@.service \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

RUN if [ -f /etc/dpkg/dpkg.cfg.d/docker ]; then \
      sed -i '\|^path-exclude /usr/share/doc/\*$|d' /etc/dpkg/dpkg.cfg.d/docker; \
    fi

STOPSIGNAL SIGRTMIN+3

CMD ["/sbin/init"]
