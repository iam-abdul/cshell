#!/usr/bin/env sh
#
# One-line installer for cshell.
#
#   curl -fsSL https://iam-abdul.github.io/cshell/install.sh | sudo sh
#
# On Debian/Ubuntu it sets up the signed apt repository (so future releases
# arrive through `apt upgrade`). On macOS it installs the prebuilt binary
# from the latest GitHub release into /usr/local/bin.
set -eu

REPO="iam-abdul/cshell"
REPO_URL="https://iam-abdul.github.io/cshell"
KEYRING="/usr/share/keyrings/cshell.gpg"
SOURCES="/etc/apt/sources.list.d/cshell.list"
PREFIX="/usr/local/bin"

# Re-exec with sudo if we are not root (needed to write apt config / $PREFIX).
if [ "$(id -u)" -ne 0 ]; then
  if command -v sudo >/dev/null 2>&1; then
    exec sudo sh "$0" "$@"
  fi
  echo "cshell: please run this script as root (or install sudo)." >&2
  exit 1
fi

# Pick a downloader (both write to stdout).
if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO- "$1"; }
else
  echo "cshell: need curl or wget to download." >&2
  exit 1
fi

# normalize the machine architecture to GoReleaser's naming.
goarch() {
  case "$(uname -m)" in
    x86_64 | amd64) echo amd64 ;;
    arm64 | aarch64) echo arm64 ;;
    *) echo "cshell: unsupported architecture $(uname -m)." >&2; exit 1 ;;
  esac
}

install_deb() {
  echo "cshell: installing the signing key -> $KEYRING"
  fetch "$REPO_URL/key.gpg" | gpg --dearmor -o "$KEYRING"

  echo "cshell: adding the apt repository -> $SOURCES"
  echo "deb [signed-by=$KEYRING] $REPO_URL stable main" > "$SOURCES"

  echo "cshell: updating package lists"
  apt-get update

  echo "cshell: installing cshell"
  apt-get install -y cshell

  echo "cshell: done. Run 'cshell' to start."
}

install_macos() {
  arch=$(goarch)

  echo "cshell: finding the latest macOS ($arch) release"
  url=$(fetch "https://api.github.com/repos/$REPO/releases/latest" \
    | grep -o "https://github.com/$REPO/releases/download/[^\"]*darwin_${arch}\.tar\.gz" \
    | head -n1)
  if [ -z "$url" ]; then
    echo "cshell: no darwin_${arch} asset in the latest release; see" >&2
    echo "        https://github.com/$REPO/releases/latest" >&2
    exit 1
  fi

  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT

  echo "cshell: downloading $url"
  fetch "$url" > "$tmp/cshell.tar.gz"
  tar -xzf "$tmp/cshell.tar.gz" -C "$tmp"

  echo "cshell: installing -> $PREFIX/cshell"
  mkdir -p "$PREFIX"
  install -m 0755 "$tmp/cshell" "$PREFIX/cshell"

  # register as a valid login shell so chsh(1) accepts it
  if [ -f /etc/shells ] && ! grep -qxF "$PREFIX/cshell" /etc/shells; then
    echo "$PREFIX/cshell" >> /etc/shells
  fi

  echo "cshell: done. Run 'cshell' to start."
}

case "$(uname -s)" in
  Linux)
    if ! command -v apt-get >/dev/null 2>&1; then
      echo "cshell: the Linux installer supports Debian/Ubuntu (apt) only." >&2
      echo "        Download a prebuilt binary from:" >&2
      echo "        https://github.com/$REPO/releases/latest" >&2
      exit 1
    fi
    install_deb
    ;;
  Darwin)
    install_macos
    ;;
  *)
    echo "cshell: unsupported OS $(uname -s). See" >&2
    echo "        https://github.com/$REPO/releases/latest" >&2
    exit 1
    ;;
esac
