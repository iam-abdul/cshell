#!/usr/bin/env sh
#
# One-line installer for cshell on Debian / Ubuntu.
#
#   curl -fsSL https://iam-abdul.github.io/cshell/install.sh | sudo sh
#
# It sets up the signed apt repository and installs cshell, so future
# releases arrive through the normal `apt upgrade`.
set -eu

REPO_URL="https://iam-abdul.github.io/cshell"
KEYRING="/usr/share/keyrings/cshell.gpg"
SOURCES="/etc/apt/sources.list.d/cshell.list"

# Re-exec with sudo if we are not root.
if [ "$(id -u)" -ne 0 ]; then
  if command -v sudo >/dev/null 2>&1; then
    exec sudo sh "$0" "$@"
  fi
  echo "cshell: please run this script as root (or install sudo)." >&2
  exit 1
fi

# We only know how to install the .deb via apt.
if ! command -v apt-get >/dev/null 2>&1; then
  echo "cshell: this installer only supports Debian/Ubuntu (apt)." >&2
  echo "        Download a prebuilt binary from:" >&2
  echo "        https://github.com/iam-abdul/cshell/releases/latest" >&2
  exit 1
fi

# Pick a downloader.
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO- "$1"; }
else
  echo "cshell: need curl or wget to download the signing key." >&2
  exit 1
fi

echo "cshell: installing the signing key -> $KEYRING"
fetch "$REPO_URL/key.gpg" | gpg --dearmor -o "$KEYRING"

echo "cshell: adding the apt repository -> $SOURCES"
echo "deb [signed-by=$KEYRING] $REPO_URL stable main" > "$SOURCES"

echo "cshell: updating package lists"
apt-get update

echo "cshell: installing cshell"
apt-get install -y cshell

echo "cshell: done. Run 'cshell' to start."
