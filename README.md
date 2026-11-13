# Library Login

A simple kiosk web app for logging students into the library.

Or logging anyone into anywhere, really.

Built with Go, works on Windows, Linux, and MacOS.

## Features

* Upload a roster of people, then allow them to log in with their unique ID.
* Record log-ins and log-outs
* Barcode-reader compatible
* Customizable settings and UI
* Web-based password-authenticated admin interface
* Searchable history

## Quick Start

Download an executable from Releases for your platform. Follow the instructions below, then launch a browser window (or use the `-open` option) to https://localhost:8765.

If opening in networked mode, the console will display the URL(s) at which the application is available.

### Windows

Simply open library-login.exe, bypass the SmartScreen prompt, then in the resulting cmd pane select local or network mode.

### Linux / MacOS

From a terminal in the installed dir, simply run:

```bash
./library-login [options]
```

#### Options

`-lan` start in networked mode

`-open` open a browser window to http://{host}:{port} on start

`-data` override default data directory

`-host` bind address (default 127.0.0.1)

`-port` bind port (default 8765)

## SECURITY NOTE

Library Login does not have any built-in support for HTTPS, so all requests/responses (including the administrator password and session cookies) will be sent unencrypted over the network.

Only use on a trusted network.

If you require more security, you'll need to set up a reverse proxy such as nginx with SSL termination in front of Library Login.

## Interface

### Kiosk `/`

The primary login/logout interface for visitors. Enter an ID in the form field and submit. Get instant success/failure feedback.

### Admin `/admin`

The administrative interface, where you can upload your roster, view activity, and customize the application.

The first time you access this interface, you'll be asked to set a password.

#### `/admin/roster`

View the roster, add people, or upload a CSV.

CSV headers are:

`id` Unique value the visitor enters into the kiosk to log in

`name` Visitor's name

`category` Optional arbitrary field, can be used to search and filter reports

#### `/admin/schedule`

View, create, or edit the period schedule.

#### `/admin/reports`

Download report CSVs, filterable by time, period, and visitor traits.

#### `/admin/settings`

See [Settings](#settings) section for details.

## Configuration

### Data Directory

By default, the SQLite database and other files are stored in the following location:

| OS              | Location                                   |
| --------------- | ------------------------------------------ |
| Linux  | `~/.config/library-login`                  |
| macOS           | `~/Library/Application Support/library-login` |
| Windows         | `%AppData%\library-login`                  |

This can be overridden with:

* `-dir <dir>` CLI argument
* `LIBRARY_LOGIN_DATA=<dir>` environment variable
* `LIBRARY_LOGIN_LOCAL=1` environment variable (uses the current working directory)

### Settings

##### Site Name

Page title

##### Timezone

Display/reporting timezone

##### Debounce

Minimum time (in seconds) between kiosk submissions

##### Auto-checkout

If enabled, the time at which any logged in visitors are automatically logged out.

##### Kiosk Heading

Banner text shown on kisok page

##### Kisok instructions

Smaller instruction text displayed below the heading

##### Kiosk Logo

File upload for an image (up to 2MB) shown above the header on the kiosk.

##### Admin password

Change admin password

##### Backup

Download a copy of the database.

## Development

```sh
cp .env.example .env
go run .
```

## Building

```sh
./build.sh 1.0.0
```

Cross-compiles release binaries and writes a zip per target to `dist/`:

- `linux/amd64`, `linux/arm64` — ChromeOS and desktop Linux
- `darwin/arm64`, `darwin/amd64` — Apple Silicon and Intel Macs
- `windows/amd64`
- `library-login_<version>_amd64.deb` and `_arm64.deb` — Debian, ChromeOS, and Raspberry Pi

Binaries are unsigned.