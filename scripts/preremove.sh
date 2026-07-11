#!/bin/sh
# Runs as the .deb prerm. Drop cshell from /etc/shells on real removal
# (but not on upgrade, where $1 is "upgrade").
set -e

if [ "$1" = "remove" ] && command -v remove-shell >/dev/null 2>&1; then
  remove-shell /usr/bin/cshell
fi
