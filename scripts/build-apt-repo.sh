#!/usr/bin/env bash
#
# Build a GPG-signed apt repository from the .deb files GoReleaser produced.
#
# Layout produced (served at https://<owner>.github.io/cshell):
#   key.gpg                                          armored public signing key
#   pool/main/c/cshell/*.deb                          the packages
#   dists/stable/Release{,.gpg} , InRelease           signed indices
#   dists/stable/main/binary-<arch>/Packages{,.gz}
#
# Usage:
#   scripts/build-apt-repo.sh <dist-dir> <out-dir> <gpg-key-id> [existing-repo-dir]
#
#   dist-dir          directory containing the freshly built *.deb (GoReleaser's ./dist)
#   out-dir           where to write the repo (published to gh-pages)
#   gpg-key-id        key id/fingerprint to sign with (already imported into the keyring)
#   existing-repo-dir optional: a checkout of the current gh-pages, so older
#                     package versions are carried forward
set -euo pipefail

DIST_DIR=${1:?dist dir required}
OUT_DIR=${2:?output dir required}
KEY_ID=${3:?gpg key id required}
EXISTING_DIR=${4:-}

ARCHES="amd64 arm64"
POOL="pool/main/c/cshell"

rm -rf "$OUT_DIR"
mkdir -p "$OUT_DIR/$POOL"
for arch in $ARCHES; do
  mkdir -p "$OUT_DIR/dists/stable/main/binary-$arch"
done

# carry forward previously published .deb files, if any
if [[ -n "$EXISTING_DIR" && -d "$EXISTING_DIR/$POOL" ]]; then
  cp -n "$EXISTING_DIR/$POOL"/*.deb "$OUT_DIR/$POOL/" 2>/dev/null || true
fi

# add the new packages
cp "$DIST_DIR"/*.deb "$OUT_DIR/$POOL/"
echo "packages in repo:"
ls -1 "$OUT_DIR/$POOL"

# apt-ftparchive tree config: scans pool/ and writes per-arch Packages indexes
cat > "$OUT_DIR/apt-ftparchive.conf" <<EOF
Dir { ArchiveDir "."; };
TreeDefault { Directory "pool/\$(SECTION)"; };
Tree "dists/stable" {
  Sections "main";
  Architectures "$ARCHES";
};
EOF

pushd "$OUT_DIR" >/dev/null

apt-ftparchive generate apt-ftparchive.conf
for arch in $ARCHES; do
  gzip -kf "dists/stable/main/binary-$arch/Packages"
done

apt-ftparchive \
  -o APT::FTPArchive::Release::Origin="cshell" \
  -o APT::FTPArchive::Release::Label="cshell" \
  -o APT::FTPArchive::Release::Suite="stable" \
  -o APT::FTPArchive::Release::Codename="stable" \
  -o APT::FTPArchive::Release::Components="main" \
  -o APT::FTPArchive::Release::Architectures="$ARCHES" \
  release dists/stable > dists/stable/Release

# detached + inline signatures apt looks for
gpg --batch --yes --default-key "$KEY_ID" -abs -o dists/stable/Release.gpg dists/stable/Release
gpg --batch --yes --default-key "$KEY_ID" --clearsign -o dists/stable/InRelease dists/stable/Release

# public key users import (armored)
gpg --armor --export "$KEY_ID" > key.gpg

rm -f apt-ftparchive.conf
popd >/dev/null

echo "apt repo built in $OUT_DIR"
