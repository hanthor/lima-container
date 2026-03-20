# syntax=docker/dockerfile:1@sha256:4a43a54dd1fedceb30ba47e76cfcf2b47304f4161c0caeac2db1c61804ea3c91
FROM ghcr.io/qemus/qemu:latest@sha256:a2a76a6b5d2304a132c7fda832670af972c1e1437d48b4bc3dea08d001b08eba

ARG TARGETARCH
ARG LIMA_VERSION=v2.1.0
ARG LIMA_UID=1000
ARG LIMA_GID=1000

USER root

RUN set -eux; \
    apt-get update; \
  apt-get --no-install-recommends -y install ca-certificates curl jq openssh-client xz-utils; \
    case "${TARGETARCH}" in \
      amd64)  LIMA_ARCH="x86_64" ;; \
      arm64)  LIMA_ARCH="aarch64" ;; \
      *)      echo "Unsupported TARGETARCH: ${TARGETARCH}"; exit 1 ;; \
    esac; \
    curl -fsSL "https://github.com/lima-vm/lima/releases/download/${LIMA_VERSION}/lima-${LIMA_VERSION#v}-Linux-${LIMA_ARCH}.tar.gz" -o /tmp/lima.tgz; \
    tar -xzf /tmp/lima.tgz -C /tmp; \
    install -m 0755 /tmp/bin/limactl /usr/local/bin/limactl; \
    install -m 0755 /tmp/bin/lima /usr/local/bin/lima; \
    mkdir -p /usr/local/share/lima; \
    cp -a /tmp/share/lima/. /usr/local/share/lima/; \
    groupadd --gid "${LIMA_GID}" lima; \
    useradd --uid "${LIMA_UID}" --gid "${LIMA_GID}" --create-home --home-dir /home/lima --shell /bin/bash lima; \
    mkdir -p /var/lib/lima /opt/lima/templates /workspace; \
    chmod 1777 /tmp; \
    chown -R lima:lima /var/lib/lima /home/lima /workspace; \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

COPY --chmod=0755 scripts/entrypoint.sh /usr/local/bin/lima-entrypoint
COPY --chmod=0755 scripts/preflight.sh /usr/local/bin/lima-preflight
COPY --chmod=0755 scripts/lima-websocket-bridge.sh /usr/local/bin/lima-websocket-bridge
COPY --chmod=0755 scripts/lima-detect-vnc-port.sh /usr/local/bin/lima-detect-vnc-port
COPY --chmod=0755 scripts/lima-use-vnc-port.sh /usr/local/bin/lima-use-vnc-port
COPY --chmod=0755 scripts/lima-up.sh /usr/local/bin/lima-up
COPY --chmod=0755 scripts/qemu-tcg-wrapper.sh /usr/local/bin/qemu-tcg-wrapper
COPY --chmod=0755 scripts/lima-as-user.sh /usr/local/bin/lima-as-user
COPY --chmod=0644 templates/*.yaml /opt/lima/templates/

ENV LIMA_HOME=/var/lib/lima
ENV LIMA_USER=lima
ENV WEB_PORT=8006
ENV WSS_PORT=5700
ENV LIMA_VNC_PORT=5901
ENV LIMA_TEMPLATE=default
ENV AUTO_START_LIMA=Y
ENV LIMA_ACCEL_MODE=auto
ENV LIMA_VNC_PORT_FILE=/run/lima-vnc-port

EXPOSE 8006

ENTRYPOINT ["/usr/local/bin/lima-entrypoint"]
