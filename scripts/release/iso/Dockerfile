# Reproducible HackMe Miner ISO build environment.
FROM ubuntu:24.04
RUN DEBIAN_FRONTEND=noninteractive apt-get update \
  && apt-get install -y --no-install-recommends \
    debootstrap squashfs-tools xorriso \
    grub-pc-bin grub-efi-amd64-bin \
    ca-certificates rsync \
  && rm -rf /var/lib/apt/lists/*
COPY build_inner.sh chroot-install.sh /build/
RUN chmod +x /build/build_inner.sh /build/chroot-install.sh
WORKDIR /build
ENTRYPOINT ["/build/build_inner.sh"]
