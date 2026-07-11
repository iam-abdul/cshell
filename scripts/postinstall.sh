#!/bin/sh
# Runs as the .deb postinst. Register cshell as a valid login shell so it
# appears in /etc/shells and chsh(1) will accept it.
set -e

if [ "$1" = "configure" ] && command -v add-shell >/dev/null 2>&1; then
  add-shell /usr/bin/cshell
fi
