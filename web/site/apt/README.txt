HackMe apt archive keyring
==========================
Install:
  curl -fsSL https://hackme.tech/apt/hackme-archive-keyring.gpg \
    | sudo tee /usr/share/keyrings/hackme-archive-keyring.gpg >/dev/null

  echo 'deb [signed-by=/usr/share/keyrings/hackme-archive-keyring.gpg] https://hackme.tech/apt stable main' \
    | sudo tee /etc/apt/sources.list.d/hackme.list

  sudo apt update && sudo apt install hackme-node

Fingerprint: C2779678AA76099672C3ACED5D8F54B6E2FC3742
