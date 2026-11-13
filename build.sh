#!/usr/bin/env bash
# Cross-compile release binaries and assemble per-target distribution zips.
# Usage: ./build.sh [version]   (version defaults to "dev")
set -euo pipefail

VERSION="${1:-dev}"
APP="library-login"
OUT="dist"
LDFLAGS="-s -w -X main.version=${VERSION}"
MAINTAINER="${MAINTAINER:-Library Login <noreply@example.com>}"

cd "$(dirname "$0")" >/dev/null
rm -rf "$OUT"
mkdir -p "$OUT"

readme() {
  case "$1" in
  windows) cat <<'EOF'
Library Login — Windows

1. Double-click library-login.exe.
   - If Windows SmartScreen says "Windows protected your PC": click "More info",
     then "Run anyway" (one time).
   - A window asks how to run:
        [1] Network - other devices (Chromebooks, tablets) can connect
        [2] Local   - only this computer
     Press 1 for a shared sign-in server, or 2 for this computer only. After the
     first time it remembers your choice and uses it automatically if you do not
     pick within 10 seconds; press Enter to accept the remembered choice now.
   - In Network mode, when Windows Firewall asks, click "Allow access" and keep
     "Private networks" checked -- otherwise other devices cannot connect.
2. In Network mode the window prints an address like:
      On your network:  http://192.168.1.42:8765/
   Enter that address in the browser on each Chromebook / device.
3. The admin area is that address followed by /admin/ ; the first visit sets a password.

Leave the window open while in use; closing it stops the server.
Data is stored in %AppData%\library-login.

Want it to start when the PC turns on? Press Win+R, type shell:startup, and put a
shortcut to library-login.exe in the folder that opens.

If Firewall is managed by IT and shows no prompt, an administrator can pre-allow the
port from an elevated Command Prompt:
   netsh advfirewall firewall add rule name="Library Login" dir=in action=allow protocol=TCP localport=8765
EOF
    ;;
  darwin) cat <<'EOF'
Library Login — macOS

1. Double-click run.command.
2. If macOS blocks it ("cannot be opened because the developer cannot be verified"),
   right-click run.command, choose Open, then Open again. You only do this once.
3. Your browser opens to the sign-in kiosk. The admin area is at the /admin/ link;
   the first visit asks you to create a password.

Leave the Terminal window open while in use; closing it stops the app.
Data is stored in ~/Library/Application Support/library-login.
EOF
    ;;
  linux) cat <<'EOF'
Library Login — ChromeOS / Linux

1. Extract this folder into your Linux files.
2. Open the Terminal, change into this folder, and run:   bash install.sh
   That makes the app executable, installs it, and starts it. Your browser opens
   to the sign-in kiosk, and "Library Login" is added to your app launcher — so
   from then on you can just click the icon (no Terminal needed).
3. Click the Admin link (or visit /admin/); the first visit asks you to create a
   password.

Leave the window open while in use; closing it stops the app.
Data is stored in ~/.config/library-login, so updating the app never loses data.

Prefer to do it by hand instead of install.sh?
   chmod +x library-login && ./library-login
EOF
    ;;
  esac
}

build() {
  local os="$1" arch="$2"
  local name="${APP}-${VERSION}-${os}-${arch}"
  local dir="${OUT}/${name}"
  local bin="${APP}"
  [ "$os" = "windows" ] && bin="${APP}.exe"

  echo "building ${os}/${arch}..."
  mkdir -p "$dir"
  CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" \
    go build -trimpath -ldflags "$LDFLAGS" -o "${dir}/${bin}" .
  readme "$os" >"${dir}/README.txt"
  if [ "$os" = "darwin" ]; then
    cp packaging/run.command "${dir}/run.command"
    chmod +x "${dir}/run.command"
  fi
  if [ "$os" = "linux" ]; then
    cp packaging/install.sh "${dir}/install.sh"
    chmod +x "${dir}/install.sh"
    cp packaging/library-login.service "${dir}/library-login.service"
  fi
  (cd "$OUT" >/dev/null && zip -qr "${name}.zip" "${name}")
  echo "  -> ${OUT}/${name}.zip"
}

# build_deb assembles a Debian package for the given architecture, reusing the
# linux binary already built by build(). ChromeOS installs these with a
# double-click; they also install on Debian/Raspberry Pi hosts.
build_deb() {
  local arch="$1"
  local pkg="${APP}_${VERSION}_${arch}"
  local root="${OUT}/${pkg}"
  local binsrc="${OUT}/${APP}-${VERSION}-linux-${arch}/${APP}"

  echo "packaging ${pkg}.deb..."
  mkdir -p "${root}/DEBIAN" "${root}/usr/bin" "${root}/usr/share/applications"
  install -m 0755 "$binsrc" "${root}/usr/bin/${APP}"
  install -m 0644 packaging/library-login.desktop "${root}/usr/share/applications/library-login.desktop"
  cat >"${root}/DEBIAN/control" <<EOF
Package: library-login
Version: ${VERSION}
Section: utils
Priority: optional
Architecture: ${arch}
Maintainer: ${MAINTAINER}
Description: Entry/exit logging kiosk
 A single-binary sign-in kiosk with an admin area for roster,
 schedule, and reports. Stores data locally in SQLite.
EOF
  dpkg-deb --build --root-owner-group "$root" "${OUT}/${pkg}.deb" >/dev/null
  echo "  -> ${OUT}/${pkg}.deb"
}

build linux amd64   # ChromeOS (Intel) + desktop Linux
build linux arm64   # ChromeOS (ARM) + desktop Linux
build darwin arm64  # Apple Silicon
build darwin amd64  # Intel Mac
build windows amd64

if command -v dpkg-deb >/dev/null 2>&1; then
  build_deb amd64
  build_deb arm64
else
  echo "note: dpkg-deb not found — skipping .deb packages (install with: brew install dpkg)"
fi

rm -rf "${OUT}"/*/ # drop intermediate staging dirs, keep only .zip and .deb
echo "done -> ${OUT}/"
