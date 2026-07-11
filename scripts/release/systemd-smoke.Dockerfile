ARG BASE_IMAGE=debian:bookworm-slim
FROM ${BASE_IMAGE}

ENV container=docker
ENV DEBIAN_FRONTEND=noninteractive

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

STOPSIGNAL SIGRTMIN+3

CMD ["/sbin/init"]
