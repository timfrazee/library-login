#!/usr/bin/env bash
# One-step install for ChromeOS / Linux: copy the binary to a stable location,
# make it executable, and launch it. The first run registers the app-launcher
# icon, so afterward it can be started by clicking "Library Login".
set -euo pipefail

src="$(cd "$(dirname "$0")" && pwd)"
dest="$HOME/.local/bin"

mkdir -p "$dest"
cp "$src/library-login" "$dest/library-login"
chmod +x "$dest/library-login"

echo "Installed to $dest/library-login"
echo "Starting Library Login — leave this window open while it is in use."
echo
exec "$dest/library-login"
