# bacnetctl — BACnet/IP CLI for Device Discovery, Monitoring and Diagnostics

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![CI Status](https://github.com/otfabric/bacnetctl/actions/workflows/ci.yml/badge.svg)](https://github.com/otfabric/bacnetctl/actions/workflows/ci.yml)
[![Code Coverage](https://codecov.io/gh/otfabric/bacnetctl/graph/badge.svg)](https://codecov.io/gh/otfabric/bacnetctl)
[![Latest Release](https://img.shields.io/github/v/release/otfabric/bacnetctl?label=release)](https://github.com/otfabric/bacnetctl/releases)

**bacnetctl is a powerful, standalone BACnet/IP command-line client
for building automation, HVAC, BMS and industrial IoT.**

Discover BACnet devices, read and write object properties, monitor
Change of Value (COV) notifications, inspect alarms and trend logs,
and troubleshoot BACnet/IP networks.

Built in pure Go using the
[OTFabric go-bacnet](https://github.com/otfabric/go-bacnet)
library, bacnetctl provides:

- BACnet/IP device discovery (Who-Is / I-Am)
- Property reads and writes
- BACnet object enumeration and inspection
- COV subscriptions and real-time monitoring
- Alarm and event handling
- BBMD and routed BACnet network diagnostics
- BACnet packet decoding
- Script-friendly JSON output
- Standalone binaries for Linux, macOS and Windows

**No runtime dependencies, no CGO and no installation of Go required.**

> **Current scope:** BACnet/IP client only. It does not simulate BACnet devices.

It is built for commissioning and point-to-point checks, troubleshooting a device or a network, integration work and scripts. Output can be plain text, a table or JSON with stable exit codes, so it works in pipelines as well as at a terminal. It is written in Go on top of [go-bacnet](https://github.com/otfabric/go-bacnet). It is a client only: it does not simulate a device.

**Quick links:** [Quick start](#quick-start) · [Features](#features) · [Installation](#installation) · [BACnet in two minutes](#bacnet-in-two-minutes) · [Addressing](#addressing-a-device) · [Commands](#common-commands) · [FAQ](#faq)

> **Responsible use:** bacnetctl sends real BACnet requests, and BACnet/IP has no authentication: whoever can reach a device can read and command it. Only use it on devices and networks you own or are authorized to work on. `write` and `release` change what a plant does (a fan, a valve, a setpoint), `time sync` changes clocks, `file put` can deliver a configuration, and `reinitialize` and `comm-control` restart or silence a device. Know what an object controls before you write to it. See [SECURITY.md](SECURITY.md).

> **Status:** version 0.1.0 is tested against a simulated device and was run by hand against five open-source BACnet stacks in containers. It has not been run against physical devices; see [What has it been tested with?](#what-has-it-been-tested-with)

---

## Why bacnetctl?

- **Find what is there** — `discover` lists the devices that answer a Who-Is, on this network, behind routers, through a BBMD, or at one address; `whohas` finds the device that holds an object by its name.
- **Read anything** — `read` takes objects and properties by name (`analog-input:1 present-value`), one or many in a request; `describe` shows every property of an object; `device` shows what a device says about itself.
- **The point list in one command** — `objects --values` lists every object with its name, present value, unit, status and description.
- **Write, carefully** — `write` takes typed values (`real:21.5`) or looks the type up, with command priorities, one property or several in one request; `release` relinquishes one. A write is sent exactly once, and a write that was not acknowledged is reported as what it is: outcome unknown.
- **Create and delete objects** — `create` asks a device for a new object, with initial values; `delete` removes one, behind an explicit confirmation.
- **Watch values live** — `monitor` subscribes to changes of value (COV) and prints each notification, or polls where COV is not supported.
- **Alarms, trends, files** — list and acknowledge alarms, listen for event notifications, read trend logs, fetch and deliver files.
- **See the network** — `router list` shows the BACnet routers and the networks behind them, `bbmd bdt` / `bbmd fdt` the tables of a BBMD.
- **Decode a capture** — `decode` takes the hex of a datagram and shows it layer by layer, offline.
- **Script-friendly** — `--format text|json|table` on every command, a single JSON error object on failure (also for a mistyped flag), stable exit codes and `BACNETCTL_*` environment variables.
- **Pre-built binaries** — Linux, macOS and Windows (x86-64 and ARM) from [GitHub Releases](https://github.com/otfabric/bacnetctl/releases), with SHA-256 checksums; no runtime to install.

---

## Quick start

Download a binary from [GitHub Releases](https://github.com/otfabric/bacnetctl/releases) (see [Installation](#installation) for copy-paste commands for Linux, macOS and Windows, or build from source). Then, with `192.168.1.20` replaced by your device:

```console
# Which devices are on this network?
bacnetctl discover

# Who is at this address?
bacnetctl device --device 192.168.1.20

# Its point list: every object with value, unit, status and description
bacnetctl objects --device 192.168.1.20 --values

# One value
bacnetctl read --device 192.168.1.20 analog-input:1 present-value

# Everything about one object
bacnetctl describe --device 192.168.1.20 analog-input:1

# Watch two points change, until Ctrl-C
bacnetctl monitor --device 192.168.1.20 analog-input:1 binary-input:3

# Override a setpoint at operator priority, then hand it back
bacnetctl write --device 192.168.1.20 analog-value:1 present-value 22.5 --priority 8
bacnetctl release --device 192.168.1.20 analog-value:1 --priority 8

# The same read as JSON, for a script
bacnetctl read --device 192.168.1.20 analog-input:1 present-value --format json
```

An object is written **type:instance** (`analog-input:1`, or short `ai:1`), a property by its name (`present-value`, or short `pv`). `export BACNETCTL_DEVICE=192.168.1.20` saves typing `--device`. See [BACnet in two minutes](#bacnet-in-two-minutes).

---

## Use cases

- **Commissioning and point-to-point checks** — List the objects of a controller, compare with the point schedule, watch a point with `monitor` while someone operates the field device.
- **Troubleshooting** — Is the device there (`discover --target`), what does it support (`device`), what does it answer exactly (`read`, `describe`), what is in alarm (`alarms`), and what is on the wire (`decode`).
- **Integration work** — Find the object behind a name (`whohas`), read its properties and units, check what a write does and at which priority it sticks.
- **Network inventory** — Devices, routers and the networks behind them, BBMDs and their tables, as JSON.
- **Automation and CI** — Use `--format json`, exit codes and environment variables in scripts and pipelines.
- **Collecting data** — Trend logs and files off a controller; a COV stream as JSON Lines into a file.

---

## Table of Contents

- [Why bacnetctl?](#why-bacnetctl)
- [Quick start](#quick-start)
- [Use cases](#use-cases)
- [Features](#features)
- [Installation](#installation)
- [BACnet in two minutes](#bacnet-in-two-minutes)
- [Usage](#usage)
  - [General CLI usage](#general-cli-usage)
  - [Addressing a device](#addressing-a-device)
  - [Common commands](#common-commands)
- [Environment variable configuration](#environment-variable-configuration)
- [FAQ](#faq)
- [Project structure](#project-structure)
- [Development](#development)
- [Version history](#version-history)
- [Contributing](#contributing)
- [License](#license)

---

## Features

| **Feature**      | **Description**                                                                                                           |
|------------------|---------------------------------------------------------------------------------------------------------------------------|
| Discover         | Who-Is / I-Am: broadcast, to one address (`--target`), through a BBMD as a foreign device (`--bbmd`), to one BACnet network behind a router (`--network`), for a range of device instances; optionally with the names of the devices. |
| Who-Has          | Find the devices that hold an object, by object name or identifier.                                                       |
| Read             | ReadProperty for one property, ReadPropertyMultiple for several properties and objects, split to the device's APDU size; array indexes; the special properties `all`, `required` and `optional`; falls back to single reads for devices without ReadPropertyMultiple. |
| Objects          | The object list of a device with names and, with `--values`, present values with units, status flags and descriptions.    |
| Describe         | Every property of one object, decoded: enumerations by name, bit strings as flag names, lists element by element.         |
| Device           | Identity and capabilities of a device: vendor, model, firmware, protocol revision, APDU size, segmentation, services and object types supported. |
| Write            | WriteProperty with typed values (`real:`, `unsigned:`, `signed:`, `bool:`, `enum:`, `string:`, `double:`, `octets:`, `bits:`, `date:`, `time:`, `object:`, `null`) or the type looked up from the device; command priorities 1-16; array indexes. Several properties in one WritePropertyMultiple request, with the write the device refused and those it did not attempt reported per row. Sent exactly once. |
| Release          | Relinquish a command priority (write NULL at that priority).                                                              |
| Create, delete   | CreateObject with an instance of your choice or the device's, and initial values; DeleteObject behind `--yes-i-am-sure`. |
| Monitor          | SubscribeCOV and SubscribeCOVProperty with COV increment, confirmed or unconfirmed; subscriptions renewed while running and cancelled on exit; `--poll` for devices without COV. |
| Alarms           | GetEventInformation, with GetAlarmSummary as fallback; AcknowledgeAlarm with the state and time stamp read from the object; a listener for event notifications. |
| Trend            | Trend log records with ReadRange by position, a page or the whole buffer.                                                 |
| Files            | AtomicReadFile / AtomicWriteFile for stream-access file objects, with the SHA-256 of the content.                         |
| Time             | TimeSynchronization and UTCTimeSynchronization, to one device or broadcast.                                               |
| Routers and BBMDs | Who-Is-Router-To-Network; devices behind a router addressed with `--network` and `--mac`; the broadcast distribution table and foreign device table of a BBMD. |
| Device management | ReinitializeDevice and DeviceCommunicationControl, behind an explicit `--yes-i-am-sure`, with an optional password that is never printed. |
| Decode           | Offline decoding of a BACnet/IP datagram from hex: BVLC, NPDU, APDU and the service with its parameters.                  |
| Names            | Object types and properties by name, short form (`ai`, `pv`) or number; `types` and `properties` list them. Engineering units, event states and status flags by name. Character strings in UTF-8, UCS-2, UCS-4 and ISO 8859-1. |
| `--format`       | Every command that prints a result accepts **`--format text\|json\|table`** (or **`BACNETCTL_OUTPUT_FORMAT`**); streams write JSON Lines. |
| `--debug`        | The diagnostics of the BACnet client (dropped and unexpected frames, routes) and the causes of an error, to stderr.       |
| Completion       | Shell completion for bash, zsh, fish and PowerShell: object types, property names and the values of flags.                |

The following tree view gives the overview of available commands.

```
bacnetctl
├── discover             Find devices with Who-Is
├── whohas               Find the devices that have an object, by name or identifier
├── read                 Read properties of objects (ReadProperty, ReadPropertyMultiple)
├── objects              List the objects of a device, with values
├── describe             Show every property of one object
├── device               Show what a device says about itself
├── trend                Read the records of a trend log (ReadRange)
├── file
│   ├── get              Read a file object into a local file
│   └── put              Write a local file into a file object
├── write                Write one or more properties (WriteProperty, WritePropertyMultiple)
├── release              Relinquish a command priority
├── create               Create an object in a device (CreateObject)
├── delete               Delete an object from a device (DeleteObject)
├── time
│   └── sync             Set the clock of a device, or of every device on the network
├── monitor              Print the values of objects as they change (SubscribeCOV, or polling)
├── alarms               List the active alarms and events of a device
├── ack                  Acknowledge an alarm
├── events               Listen for event notifications
├── router
│   └── list             Find the BACnet routers and the networks behind them
├── bbmd
│   ├── bdt              Read the broadcast distribution table of a BBMD
│   └── fdt              Read the foreign device table of a BBMD
├── reinitialize         Restart a device (ReinitializeDevice)
├── comm-control         Silence a device, or let it speak again
├── decode               Decode a captured BACnet/IP datagram (offline)
├── types                List the object types bacnetctl knows by name
├── properties           List the property identifiers bacnetctl knows by name
├── completion           Generate shell completion script (bash|zsh|fish|powershell)
└── version              Print version
```

## Installation

The easiest way to install bacnetctl is to download a pre-built binary from the [GitHub Releases page](https://github.com/otfabric/bacnetctl/releases). No Go toolchain is required.

| Platform | CPU | Download |
|---|---|---|
| Linux | x86-64 | `bacnetctl-<version>-linux-amd64.tar.gz` |
| Linux | 64-bit ARM (Raspberry Pi 4/5 with a 64-bit OS, AWS Graviton) | `bacnetctl-<version>-linux-arm64.tar.gz` |
| Linux | 32-bit ARM (Raspberry Pi with a 32-bit OS) | `bacnetctl-<version>-linux-armv7.tar.gz` |
| macOS | Apple Silicon (M1 and later) | `bacnetctl-<version>-darwin-arm64.tar.gz` |
| macOS | Intel | `bacnetctl-<version>-darwin-amd64.tar.gz` |
| Windows | x86-64 | `bacnetctl-<version>-windows-amd64.zip` |
| Windows | ARM64 | `bacnetctl-<version>-windows-arm64.zip` |

Each release also contains `bacnetctl-<version>-checksums.txt` (SHA-256) so you can verify your download. The commands below fetch the **latest** release, verify its checksum and install it. Afterwards, check that it works:

```console
bacnetctl version
```

### Linux

```bash
# 1. Find the latest version and your CPU architecture
VERSION=$(curl -fsSLI -o /dev/null -w '%{url_effective}' https://github.com/otfabric/bacnetctl/releases/latest | sed 's|.*/v||')
case "$(uname -m)" in
  x86_64)         ARCH=amd64 ;;
  aarch64|arm64)  ARCH=arm64 ;;
  armv7l)         ARCH=armv7 ;;
  *) echo "Unsupported architecture: $(uname -m)" ;;
esac

# 2. Download the archive and the checksum file
FILE="bacnetctl-${VERSION}-linux-${ARCH}.tar.gz"
BASE="https://github.com/otfabric/bacnetctl/releases/download/v${VERSION}"
curl -fsSLO "${BASE}/${FILE}"
curl -fsSLO "${BASE}/bacnetctl-${VERSION}-checksums.txt"

# 3. Verify the download (should print "OK")
grep "${FILE}" "bacnetctl-${VERSION}-checksums.txt" | sha256sum -c -

# 4. Extract and install (the binary in the archive carries the version and platform in its name)
mkdir -p bacnetctl-dl && tar -xzf "${FILE}" -C bacnetctl-dl
sudo install -m 0755 "$(find bacnetctl-dl -type f -name 'bacnetctl*' | head -n1)" /usr/local/bin/bacnetctl
```

No `sudo`? Install into your home directory instead (and make sure `~/.local/bin` is in your `PATH`):

```bash
mkdir -p ~/.local/bin
install -m 0755 "$(find bacnetctl-dl -type f -name 'bacnetctl*' | head -n1)" ~/.local/bin/bacnetctl
```

### macOS

```bash
# 1. Find the latest version and your CPU architecture (arm64 = Apple Silicon, x86_64 = Intel)
VERSION=$(curl -fsSLI -o /dev/null -w '%{url_effective}' https://github.com/otfabric/bacnetctl/releases/latest | sed 's|.*/v||')
case "$(uname -m)" in
  arm64)   ARCH=arm64 ;;
  x86_64)  ARCH=amd64 ;;
esac

# 2. Download the archive and the checksum file
FILE="bacnetctl-${VERSION}-darwin-${ARCH}.tar.gz"
BASE="https://github.com/otfabric/bacnetctl/releases/download/v${VERSION}"
curl -fsSLO "${BASE}/${FILE}"
curl -fsSLO "${BASE}/bacnetctl-${VERSION}-checksums.txt"

# 3. Verify the download (should print "OK")
grep "${FILE}" "bacnetctl-${VERSION}-checksums.txt" | shasum -a 256 -c -

# 4. Extract and install (the binary in the archive carries the version and platform in its name)
mkdir -p bacnetctl-dl && tar -xzf "${FILE}" -C bacnetctl-dl
sudo install -m 0755 "$(find bacnetctl-dl -type f -name 'bacnetctl*' | head -n1)" /usr/local/bin/bacnetctl
```

If you downloaded the archive with a web browser instead of `curl` and macOS refuses to run the binary ("cannot be verified"), remove the quarantine flag: `xattr -d com.apple.quarantine /usr/local/bin/bacnetctl`.

### Windows (PowerShell)

```powershell
# 1. Find the latest version and your CPU architecture
$version = (Invoke-RestMethod https://api.github.com/repos/otfabric/bacnetctl/releases/latest).tag_name.TrimStart('v')
$arch    = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }

# 2. Download the archive and the checksum file
$file = "bacnetctl-$version-windows-$arch.zip"
$base = "https://github.com/otfabric/bacnetctl/releases/download/v$version"
Invoke-WebRequest "$base/$file" -OutFile "$env:TEMP\$file"
Invoke-WebRequest "$base/bacnetctl-$version-checksums.txt" -OutFile "$env:TEMP\bacnetctl-checksums.txt"

# 3. Verify the download (throws an error on mismatch)
$expected = ((Select-String -Path "$env:TEMP\bacnetctl-checksums.txt" -Pattern $file).Line -split '\s+')[0]
$actual   = (Get-FileHash "$env:TEMP\$file" -Algorithm SHA256).Hash
if ($expected -ne $actual) { throw "Checksum mismatch for $file" }

# 4. Extract to a per-user folder and name the program bacnetctl.exe
#    (the .exe in the archive carries the version and platform in its name)
$dir = "$env:LOCALAPPDATA\Programs\bacnetctl"
New-Item -ItemType Directory -Force -Path $dir | Out-Null
Expand-Archive "$env:TEMP\$file" -DestinationPath "$env:TEMP\bacnetctl-dl" -Force
$exe = Get-ChildItem "$env:TEMP\bacnetctl-dl" -Recurse -Filter 'bacnetctl*.exe' | Select-Object -First 1
Copy-Item $exe.FullName "$dir\bacnetctl.exe" -Force

# 5. Add bacnetctl to your user PATH (then open a new terminal)
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ($userPath -notlike "*$dir*") {
  [Environment]::SetEnvironmentVariable('Path', "$userPath;$dir", 'User')
}
```

If Windows SmartScreen warns about an unrecognised app, choose **More info → Run anyway**.

### Manual download

1. Open the [latest release](https://github.com/otfabric/bacnetctl/releases/latest).
2. Download the archive for your platform from the table above.
3. Extract it, rename the binary to `bacnetctl` (`bacnetctl.exe` on Windows) and move it to a folder on your `PATH`.

### Update

Run the same commands again. They always fetch the latest release and overwrite the previous binary.

### Uninstall

bacnetctl is one file and keeps no configuration or state. Delete the binary: `sudo rm /usr/local/bin/bacnetctl` (or `rm ~/.local/bin/bacnetctl`) on Linux and macOS; on Windows remove the folder `%LOCALAPPDATA%\Programs\bacnetctl` and its entry in your user `PATH`.

### Shell completion

Completion knows the commands, the object types, the property names and the values of flags. Install it once:

```bash
# bash (Linux; needs the bash-completion package)
bacnetctl completion bash | sudo tee /etc/bash_completion.d/bacnetctl > /dev/null

# zsh (macOS and Linux): a directory on your fpath, then restart the shell
mkdir -p ~/.zsh/completions && bacnetctl completion zsh > ~/.zsh/completions/_bacnetctl
# and once in ~/.zshrc:  fpath=(~/.zsh/completions $fpath); autoload -U compinit && compinit

# fish
bacnetctl completion fish > ~/.config/fish/completions/bacnetctl.fish
```

```powershell
# PowerShell: add this line to your profile ($PROFILE)
bacnetctl completion powershell | Out-String | Invoke-Expression
```

### Firewall and privileges

bacnetctl needs no administrator rights: it opens an ordinary UDP socket. Commands that talk to one device use a free local port. `discover`, `whohas`, `router list` and `events` listen on UDP 47808, because devices answer broadcasts there: allow inbound UDP 47808 in the host firewall (Windows asks on first use), and stop other BACnet software on the same computer, which holds that port. See the [FAQ](#udp-port-47808-is-in-use-what-now).

### Build from source

Requires Go 1.25 or later:

```console
git clone https://github.com/otfabric/bacnetctl.git
cd bacnetctl
make build
```

`make build` runs the checks and writes the binary to **`bin/bacnetctl`** (see [Building](#building)). With a Go toolchain you can also install straight from the module:

```console
go install github.com/otfabric/bacnetctl@latest
```

---

## BACnet in two minutes

BACnet (ANSI/ASHRAE 135, ISO 16484-5) is the protocol of building automation: air handling units, boilers, room controllers, meters, lighting. BACnet/IP carries it over UDP, by default on port **47808** (hexadecimal BAC0). There is no connection: every device has one socket and sends datagrams to the others, and it finds them by broadcast.

| Term | Meaning | In bacnetctl |
|---|---|---|
| Device | One controller on the network. It has a **device instance**, a number from 0 to 4194302 that is unique in the whole BACnet internetwork | `--device host[:port]` is its address; the instance is shown by `discover` and `device` |
| Object | One thing a device holds: an input, an output, a setpoint, a schedule, a trend log, and the device itself. It has a type and an instance number | `analog-input:1`, `ai:1`; `bacnetctl types` |
| Property | One fact about an object: `present-value`, `object-name`, `units`, `status-flags`, `description` | `present-value`, `pv`; `bacnetctl properties` |
| Who-Is / I-Am | "Who is there?", sent by broadcast; every device answers with its instance, its largest APDU and its vendor | `discover` |
| ReadProperty, ReadPropertyMultiple | Read one property, or many properties of many objects in one request | `read`, `describe`, `objects`, `device` |
| WriteProperty | Write one property | `write`, `release` |
| Command priority | A commandable property (the present value of an output, mostly) keeps one value for each of 16 priorities, and takes the one with the highest priority (1 is highest). Writing NULL at a priority *relinquishes* it | `--priority`, `release` |
| COV | Change of value: a device notifies a subscriber when a value changes by more than the object's increment | `monitor` |
| APDU, segmentation | The application message. A device accepts APDUs up to a size (50 to 1476 octets) and may or may not split larger ones in segments | handled automatically; shown by `discover` and `device` |
| BACnet network, router | BACnet networks have numbers. A router connects them: BACnet/IP to MS/TP (the RS-485 field bus), for example. A device behind a router is addressed by network number and MAC address | `--network`, `--mac`, `router list` |
| BBMD, foreign device | IP routers do not pass broadcasts on. A BBMD (BACnet/IP broadcast management device) carries them between IP subnets. A device on a subnet without a BBMD registers with one as a *foreign device* | `--bbmd`, `bbmd bdt`, `bbmd fdt` |

A value on the wire has a type: REAL, Unsigned, Enumerated, CharacterString, BitString and so on. A device refuses a write with the wrong one, which is why `write` either takes the type (`real:21.5`) or looks it up.

---

## Usage

### General CLI Usage

```console
bacnetctl [command] [flags]

bacnetctl discover
bacnetctl whohas
bacnetctl read OBJECT PROPERTY[,PROPERTY...] [OBJECT PROPERTY...]
bacnetctl objects
bacnetctl describe OBJECT
bacnetctl device
bacnetctl trend OBJECT
bacnetctl file get OBJECT
bacnetctl file put OBJECT
bacnetctl write OBJECT PROPERTY VALUE [OBJECT PROPERTY VALUE...]
bacnetctl release OBJECT
bacnetctl create TYPE[:INSTANCE] [PROPERTY=VALUE...]
bacnetctl delete OBJECT
bacnetctl time sync
bacnetctl monitor OBJECT [OBJECT...]
bacnetctl alarms
bacnetctl ack OBJECT
bacnetctl events
bacnetctl router list
bacnetctl bbmd bdt
bacnetctl bbmd fdt
bacnetctl reinitialize STATE
bacnetctl comm-control STATE
bacnetctl decode HEX...
bacnetctl types
bacnetctl properties
bacnetctl version
```

Every command has `--help`, with a description and examples.

**Global flag:** `--debug` — log the diagnostics of the BACnet client to stderr (frames that were dropped or unexpected, routes that were learned, the socket that was bound) and print the chain of causes after an error.

**Output format (`--format`):** every command that prints a result accepts **`--format text`** (default), **`json`** or **`table`**, or set **`BACNETCTL_OUTPUT_FORMAT`**. It controls **only** the result on **stdout**. Progress, summaries and diagnostics go to **stderr**, so stdout can be piped or parsed as it is. The JSON shapes are a stable contract: see [docs/json-output.md](docs/json-output.md).

**Streams:** `monitor` and `events` write one record as it happens: one line (**text**), one JSON object per line (**json**, JSON Lines) or table rows (**table**). They run until interrupted with Ctrl-C, which ends them normally.

**Fatal errors:** with `--format json` a command writes a **single JSON error object** to stdout on failure (fields `kind`, `code`, `message`, and `bacnet` with what the device answered); with **text** or **table** it writes a single **`error:`** line to stderr and leaves stdout empty. That holds for every failure, including the ones before a command runs: an unknown flag or command, a missing argument, a malformed environment variable. Exit codes are stable:

| Code | Meaning |
|------|---------|
| 0 | Success |
| 2 | Usage: unknown command or flag, wrong number of arguments, a missing `--yes-i-am-sure`. Nothing was sent |
| 3 | Invalid input: nothing was sent |
| 4 | Transport: the UDP port is in use, a host name does not resolve, a BBMD refuses the registration |
| 5 | Timeout: no answer; for a write, the outcome is unknown |
| 6 | The device refused the request (Error, Reject, Abort), or a protocol error |
| 7 | Partial success: a valid result that holds failures |
| 10 | Output or internal error |

See [docs/exitcodes.md](docs/exitcodes.md) for the details and the `code` values.

**Shell completion:** `bacnetctl completion bash` (or `zsh`, `fish`, `powershell`) prints a completion script; for example `source <(bacnetctl completion bash)`. Object arguments complete to the object types, property arguments to the property names, flags with a fixed set of values to those values.

### Addressing a device

| Flags | Addresses |
|---|---|
| `--device host[:port]` | The device at that address; UDP port 47808 when omitted. An IPv4 address, or a host name that resolves to one |
| `--device router[:port] --network N --mac HEX` | The device with MAC address `HEX` on BACnet network `N`, behind the router at `--device`. For MS/TP the MAC is the station number in hex (`0a` for station 10) |

The device instance is not needed: bacnetctl addresses a device by where it is. `device` and `discover` show the instance; `read … device object-identifier` reads it.

**The local port.** bacnetctl opens one UDP socket per invocation.

| Commands | `--local-port` default | Why |
|---|---|---|
| `read`, `write`, `release`, `objects`, `describe`, `device`, `monitor`, `alarms`, `ack`, `trend`, `file`, `time sync`, `bbmd`, `reinitialize`, `comm-control` | 0: a free port chosen by the system | A device answers a request to the address it came from, and sends COV notifications to the subscriber's address. Any port works, and nothing collides with another BACnet program on the host |
| `discover`, `whohas`, `events`, `router list` | 47808 | Devices answer a Who-Is, and send event notifications, by **broadcast to port 47808**. Only a socket bound to that port hears them |

Only one program on a host can hold port 47808. If a BACnet workstation, gateway or another bacnetctl has it, the second group fails with exit code 4 and says so; the first group is unaffected.

| Flag | Meaning |
|---|---|
| `--local-port` | Local UDP port to bind; 0 for a free port |
| `--interface` | Restrict broadcasts to the subnet of this network interface. By default a broadcast goes to the broadcast address of every local subnet and to 255.255.255.255 |
| `--bbmd host[:port]`, `--bbmd-ttl` | Register as a foreign device with this BBMD and send broadcasts through it. bacnetctl waits for the BBMD to accept before it goes on. The registration lapses by itself after the time-to-live (60 s by default) |
| `--timeout` | APDU timeout in milliseconds: how long one request waits for its answer (3000 by default) |
| `--retries` | How often an unanswered **read** is sent again (3 by default). A write is never sent twice |

### Common Commands

The output below is from the simulated device of the test suite (an air handling unit controller with ten objects), at `127.0.0.1:47809`.

#### Find devices

```console
# Broadcast on the local network
bacnetctl discover

# A range of device instances, with the names of the devices
bacnetctl discover --low 1000 --high 1999 --names

# One address: another subnet, a container, a network that filters broadcasts
bacnetctl discover --target 10.0.20.15

# Through the BBMD of another subnet
bacnetctl discover --bbmd 10.0.20.1

# One BACnet network behind a router
bacnetctl discover --network 2001
```

```console
$ bacnetctl discover --target 127.0.0.1:47809 --local-port 0 --network all --wait 300 --names
2 devices found in 0.3 s (Who-Is to 127.0.0.1:47809)

INSTANCE  ADDRESS          NETWORK  MAC  VENDOR  MAX APDU  SEGMENTATION     NAME                VENDOR NAME  MODEL
1234      127.0.0.1:47809  -        -    999     1476      no-segmentation  AHU-1 Controller    OT Fabric    bacsim
2001010   127.0.0.1:47809  2001     0a   999     1476      no-segmentation  Zone Controller 10  OT Fabric    bacsim
```

The second device is behind a router: `ADDRESS` is the router, `NETWORK` and `MAC` are the device. (`--local-port 0` is used here because the simulated device answers to the address that asked; a real device usually answers by broadcast, which needs the default port 47808.) `--wait` is how long answers are collected (3000 ms by default). A network without devices is not an error: the result is empty and the exit code 0.

`whohas` asks for an object instead of a device:

```console
$ bacnetctl whohas --name "Fan Enable" --target 127.0.0.1:47809 --local-port 0
1 answer for name "Fan Enable" in 0.0 s

DEVICE  ADDRESS          NETWORK  MAC  OBJECT          NAME
1234    127.0.0.1:47809  -        -    binary-value:1  Fan Enable
```

#### Look at a device

```console
$ bacnetctl device --device 127.0.0.1:47809
Device:                device:1234
Address:               127.0.0.1:47809
Name:                  AHU-1 Controller
Description:           Simulated air handling unit controller
Location:              Plant room 1
Vendor:                OT Fabric
Vendor ID:             999
Model:                 bacsim
Firmware:              1.4.2
Application software:  ahu-demo 2026.10
System status:         operational (0)
Protocol version:      1
Protocol revision:     22
Max APDU:              1476
Segmentation:          no-segmentation (3)
APDU timeout:          3000 ms
APDU retries:          3
Database revision:     7
Local date:            2026-10-10
Local time:            12:00:00.00
UTC offset:            -60 min
Objects:               10

Services supported (19):
  acknowledge-alarm, subscribe-cov, atomic-read-file, atomic-write-file, read-property,
  read-property-multiple, write-property, device-communication-control,
  reinitialize-device, i-am, i-have, unconfirmed-cov-notification, time-synchronization,
  who-has, who-is, read-range, utc-time-synchronization, subscribe-cov-property,
  get-event-information

Object types supported (9):
  analog-input, analog-output, analog-value, binary-input, binary-value, device, file,
  multi-state-value, trend-log
```

```console
$ bacnetctl objects --device 127.0.0.1:47809 --values
device:1234 at 127.0.0.1:47809: 10 objects

OBJECT               NAME                 VALUE         STATUS    DESCRIPTION
device:1234          AHU-1 Controller     -             -         Simulated air handling unit controller
analog-input:1       Supply Air Temp      21.5 °C       ok        AHU-1 supply air temperature
analog-input:2       Return Air Humidity  71.25 %RH     in-alarm  AHU-1 return air humidity
analog-output:1      Heating Valve        0 %           ok        AHU-1 heating coil valve
analog-value:1       Supply Air Setpoint  20 °C         ok        AHU-1 supply air temperature setpoint
binary-input:1       Filter Alarm         inactive (0)  ok        Filterüberwachung AHU-1
binary-value:1       Fan Enable           active (1)    ok        AHU-1 supply fan enable
multi-state-value:1  Operating Mode       2             ok        AHU-1 operating mode
trend-log:1          Supply Air Temp Log  -             -         15 minute log of analog-input:1
file:1               config.txt           -             -         Controller configuration
```

`STATUS` is `ok` when no status flag is set, and otherwise the flags that are (`in-alarm`, `fault`, `overridden`, `out-of-service`). `--type ai,bi` limits the list to some object types, `--limit` to a number of objects. A large device takes a while: the list is read in as many requests as the device's APDU size needs.

#### Read properties

```console
# One value: printed bare, ready for a script
bacnetctl read --device 192.168.1.20 analog-input:1 present-value

# Several properties, several objects
bacnetctl read --device 192.168.1.20 ai:1 pv,units,status-flags av:2 pv,description

# The Device object of whatever is at this address
bacnetctl read --device 192.168.1.20 device object-identifier,object-name

# An element of an array; index 0 is the number of elements
bacnetctl read --device 192.168.1.20 analog-value:1 priority-array[8]

# Every property the device has for an object
bacnetctl read --device 192.168.1.20 ai:1 all
```

```console
$ bacnetctl read --device 127.0.0.1:47809 analog-input:1 present-value
21.5

$ bacnetctl read --device 127.0.0.1:47809 ai:1 present-value,units,status-flags,object-name --format table
OBJECT          PROPERTY       VALUE
-               -              -
analog-input:1  present-value  21.5 °C
analog-input:1  units          degrees-celsius (62)
analog-input:1  status-flags   {}
analog-input:1  object-name    Supply Air Temp
```

A present value is shown with its unit when `units` is read along with it. With several properties, one that the device refuses is reported in its row and the others are still read; the exit code is then 7:

```console
$ bacnetctl read --device 127.0.0.1:47809 ai:1 pv,high-limit --format json
{
  "target": "127.0.0.1:47809",
  "service": "read-property-multiple",
  "count": 2,
  "failed": 1,
  "rows": [
    {
      "object": "analog-input:1",
      "object_type": 0,
      "instance": 1,
      "property": "present-value",
      "property_id": 85,
      "value": {
        "type": "real",
        "value": 21.5
      }
    },
    {
      "object": "analog-input:1",
      "object_type": 0,
      "instance": 1,
      "property": "high-limit",
      "property_id": 45,
      "error": {
        "kind": "device_error",
        "code": "unknown_property",
        "message": "unknown-property",
        "bacnet": {
          "pdu": "error",
          "class": "property",
          "class_id": 2,
          "code": "unknown-property",
          "code_id": 32
        }
      }
    }
  ]
}
```

`describe` reads everything about one object:

```console
$ bacnetctl describe --device 127.0.0.1:47809 analog-value:1
analog-value:1 at 127.0.0.1:47809 (11 properties)

object-identifier:   analog-value:1
object-name:         Supply Air Setpoint
object-type:         analog-value (2)
present-value:       20 °C
description:         AHU-1 supply air temperature setpoint
status-flags:        {}
event-state:         normal (0)
out-of-service:      false
units:               degrees-celsius (62)
priority-array:      [null, null, null, null, null, null, null, null, null, null, null, null, null, null, null, null]
relinquish-default:  20
```

#### Write a property

**This changes the device.**

```console
# A typed value
bacnetctl write --device 192.168.1.20 analog-value:1 present-value real:21.5

# The type looked up from the device (one extra read)
bacnetctl write --device 192.168.1.20 analog-value:1 present-value 21.5

# A binary output on, at operator priority
bacnetctl write --device 192.168.1.20 binary-output:2 present-value active --priority 8

# A string with spaces
bacnetctl write --device 192.168.1.20 av:1 object-name "string:Setpoint Room 4"

# A negative number without a type goes after --
bacnetctl write --device 192.168.1.20 av:7 present-value -- -5
```

| Value | BACnet type |
|---|---|
| `real:21.5`, `double:21.5` | REAL (32 bit), Double (64 bit) |
| `unsigned:3`, `signed:-1` | Unsigned Integer, Signed Integer |
| `bool:true` | BOOLEAN |
| `enum:1`, `enum:active` | Enumerated: a number, or a name the property knows |
| `string:Room 4` | CharacterString (UTF-8) |
| `octets:0a1b2c`, `bits:1011` | OctetString (hex), BitString (first bit first) |
| `date:2026-10-10`, `time:12:00:00` | Date, Time |
| `object:analog-input:1` | Object identifier |
| `null` | NULL: relinquishes, with `--priority` |

Without a type, bacnetctl reads the property first and writes the value with the type it holds now; that cannot work for a property that holds NULL or a list. `--type` names the type for a value without prefix and skips the read.

```console
$ bacnetctl write --device 127.0.0.1:47809 analog-value:1 present-value 22.5 --priority 8
analog-value:1 present-value = 22.5 at priority 8 (acknowledged by the device)

$ bacnetctl read --device 127.0.0.1:47809 av:1 pv,priority-array
OBJECT          PROPERTY        VALUE
analog-value:1  present-value   22.5
analog-value:1  priority-array  [null, null, null, null, null, null, null, 22.5, null, null, null, null, null, null, null, null]

$ bacnetctl release --device 127.0.0.1:47809 av:1 --priority 8
analog-value:1 present-value: relinquished at priority 8 (acknowledged by the device)

$ bacnetctl write --device 127.0.0.1:47809 av:1 pv real:99
error: the device refused the request: value-out-of-range (error class property)
```

The last command exits with code 6. Everything that can be checked is checked before anything is sent, and a write is sent exactly once. See [Why does a write end with "outcome unknown"?](#why-does-a-write-end-with-outcome-unknown)

**Several properties in one request.** Repeat `OBJECT PROPERTY VALUE`, and bacnetctl sends one WritePropertyMultiple; `--priority` and `--type` then hold for every write. The device carries the writes out in the order given and stops at the first one it refuses, so the result has a status per write:

```console
$ bacnetctl write --device 127.0.0.1:47809 av:1 pv real:21.5 av:1 object-name "string:Supply SP" bv:1 pv active
OBJECT          PROPERTY       VALUE       PRIORITY  STATUS
analog-value:1  present-value  21.5        -         written
analog-value:1  object-name    Supply SP   -         written
binary-value:1  present-value  active (1)  -         written
3 properties written (acknowledged by the device)

$ bacnetctl write --device 127.0.0.1:47809 av:1 pv real:22 ai:1 units enum:3 av:1 description string:Checked
OBJECT          PROPERTY       VALUE        PRIORITY  STATUS
analog-value:1  present-value  22           -         written
analog-input:1  units          amperes (3)  -         failed: write-access-denied
analog-value:1  description    Checked      -         not-written
1 of 3 properties written: the device refused one, and did not attempt the 1 after it
```

The second command exits with code 7 (partial success): what was written before the refusal stays written. A device without WritePropertyMultiple rejects the request (code 6) and nothing is written; bacnetctl does not quietly turn it into single writes. When a device refuses without saying which write failed (BACpypes3 does), the whole command fails with code 6 and the state of the properties has to be read back.

#### Create and delete objects

**This changes the device.** Whether an object type can be created, and what can be deleted, is the device's decision: many devices create nothing, and most delete only what was created.

```console
# A new analog value; the device chooses the instance
$ bacnetctl create --device 127.0.0.1:47809 analog-value --name "Spare Setpoint"
analog-value:2: created (acknowledged by the device)

# A given instance, with initial values (each says its type: the object does not exist yet)
$ bacnetctl create --device 127.0.0.1:47809 av:40 --name "Room 4 Offset" description=string:Operator present-value=real:0
analog-value:40: created (acknowledged by the device)

# Delete asks for a confirmation on the command line
$ bacnetctl delete --device 127.0.0.1:47809 av:40
error: delete removes analog-value:40 from the device, with everything that refers to it left dangling: confirm with --yes-i-am-sure (nothing was sent)

$ bacnetctl delete --device 127.0.0.1:47809 av:40 --yes-i-am-sure
analog-value:40: deleted (acknowledged by the device)

$ bacnetctl delete --device 127.0.0.1:47809 av:1 --yes-i-am-sure
error: the device refused the request: object-deletion-not-permitted (error class object)
```

A device that refuses one of the initial values creates nothing. Like a write, both requests are sent exactly once: without an answer the outcome is unknown (exit code 5), and `bacnetctl objects` tells whether the object is there.

#### Monitor values

```console
# Two points, until Ctrl-C
bacnetctl monitor --device 192.168.1.20 analog-input:1 binary-input:3

# One property with an increment, ten notifications, as JSON Lines
bacnetctl monitor --device 192.168.1.20 ai:1 --property present-value --increment 0.5 --count 10 --format json

# A device without COV: poll every 2 seconds, print changes
bacnetctl monitor --device 192.168.1.20 ai:1 --poll 2000
```

```console
$ bacnetctl monitor --device 127.0.0.1:47809 analog-value:1 binary-value:1 --count 4
subscribed to analog-value:1 at 127.0.0.1:47809 (lifetime 300 s)
subscribed to binary-value:1 at 127.0.0.1:47809 (lifetime 300 s)
14:17:19.591  analog-value:1      present-value=20  status-flags={}
14:17:19.592  binary-value:1      present-value=active (1)  status-flags={}
14:17:19.765  analog-value:1      present-value=21  status-flags={}
14:17:19.778  binary-value:1      present-value=inactive (0)  status-flags={}
127.0.0.1:47809: 4 notifications in 0.2 s
```

The first line of each object is the value at the moment of subscribing; the others are changes (here, writes from another terminal). The `subscribed` and summary lines go to stderr. Text and table show the local time of day; JSON carries the full time stamp in UTC. When monitoring ends, the subscriptions are cancelled on the device.

#### Alarms and events

```console
$ bacnetctl alarms --device 127.0.0.1:47809
1 active event at 127.0.0.1:47809 (get-event-information)

OBJECT          STATE       UNACKNOWLEDGED  NOTIFY TYPE  LAST TRANSITIONS
analog-input:2  high-limit  to-offnormal    alarm        2026-10-10 09:30:00.00 | sequence 0 | 2026-10-09 18:05:00.00

$ bacnetctl ack --device 127.0.0.1:47809 analog-input:2 --source "J. Smith"
analog-input:2: high-limit transition of 2026-10-10 09:30:00.00 acknowledged as "J. Smith"
```

`LAST TRANSITIONS` are the time stamps of the last to-offnormal, to-fault and to-normal transitions. `ack` reads the state and the time stamp it has to quote from the object; `--state`, `--sequence` and `--event-time` give them instead. **`ack` changes the device.**

`events` listens for the event notifications devices send, on UDP port 47808. It only hears what is broadcast or sent to this computer: a device notifies the recipients of its notification classes.

```console
bacnetctl events
bacnetctl events --duration 3600 --format json > events.jsonl
```

#### Trend logs and files

```console
$ bacnetctl trend --device 127.0.0.1:47809 trend-log:1 --all
trend-log:1 at 127.0.0.1:47809: 7 records from position 1 (7 in the buffer)

POSITION  TIMESTAMP               KIND    VALUE              STATUS
1         2026-10-10 10:00:00.00  value   20.75              ok
2         2026-10-10 10:15:00.00  value   21                 ok
3         2026-10-10 10:30:00.00  value   21.25              ok
4         2026-10-10 10:45:00.00  value   21.5               ok
5         2026-10-10 11:00:00.00  value   21.5               ok
6         2026-10-10 11:15:00.00  value   21.75              ok
7         2026-10-10 11:30:00.00  status  {log-interrupted}  -
```

Without `--all`, `--start` and `--count` select a page; a device returns what fits one answer, and the result says when more follow. Time stamps are the device's local time.

```console
# Fetch a file object, or pipe it
bacnetctl file get --device 192.168.1.20 file:1 --output config.bin
bacnetctl file get --device 192.168.1.20 file:2 --output - | less

# Deliver a file (this changes the device)
bacnetctl file put --device 192.168.1.20 file:1 --input config.bin
```

The result names the length and the SHA-256 of the content. `--chunk` is the number of octets per request (256 by default): lower it for a device with a small APDU, raise it for speed.

#### Time synchronization

```console
# The clock of one device, set to the time of this computer (this changes the device)
bacnetctl time sync --device 192.168.1.20

# Every device on the local network, in UTC
bacnetctl time sync --broadcast --utc
```

The service is unconfirmed: a device does not answer it. `bacnetctl device` shows the local date and time a device has.

#### Routers and BBMDs

```console
$ bacnetctl router list --target 127.0.0.1:47809 --local-port 0
1 route found in 0.0 s

NETWORK  ROUTER           STATE
2001     127.0.0.1:47809  available

$ bacnetctl read --device 127.0.0.1:47809 --network 2001 --mac 0a device object-name,model-name
OBJECT          PROPERTY     VALUE
device:2001010  object-name  Zone Controller 10
device:2001010  model-name   bacsim
```

`router list` without `--target` asks by broadcast. `bbmd bdt` and `bbmd fdt` read the broadcast distribution table and the foreign device table of the BBMD at `--device`.

#### Restart or silence a device

`reinitialize` (coldstart, warmstart, activate-changes, and the backup and restore steps) and `comm-control` (disable, disable-initiation, enable) **change how a device runs**. They refuse to run without `--yes-i-am-sure`, and have sent nothing then (exit code 2).

```console
bacnetctl reinitialize warmstart --device 192.168.1.20 --yes-i-am-sure
bacnetctl comm-control disable --duration 10 --device 192.168.1.20 --yes-i-am-sure
bacnetctl comm-control enable --device 192.168.1.20 --yes-i-am-sure
```

A device may ask for a password: `--password`, or the environment variable `BACNETCTL_PASSWORD`, which keeps it out of the shell history. BACnet sends the password in clear text. bacnetctl never prints it.

#### Decode a captured frame

```console
$ bacnetctl decode 810a0017 0100 3003 0c 0c00000001 1955 3e 4441ac0000 3f
BVLC     Original-Unicast-NPDU
  function  0x0A
  length    23
NPDU     application message
  version          1
  control          0x00
  priority         normal
  expecting_reply  false
APDU     Complex-ACK
  invoke_id  3
  service    read-property (12)
SERVICE  read-property ack
  object    analog-input:1
  property  present-value (85)
  value     21.5 (real)
```

The hex is the UDP payload, as copied from a packet capture; spaces, colons and `0x` prefixes are ignored, and `-` reads it from stdin. Nothing is sent.

---

## Environment Variable Configuration

Every flag that is part of a command's configuration can also be set through an environment variable, which is convenient for automation and for flags you would otherwise repeat on every command (`BACNETCTL_DEVICE`, `BACNETCTL_OUTPUT_FORMAT`). A flag on the command line wins over the environment. A value that cannot be parsed (a port that is not a number) stops bacnetctl at startup. The global flag `--debug` and the confirmation `--yes-i-am-sure` have no environment variable.

```console
export BACNETCTL_DEVICE=192.168.1.20
bacnetctl device
bacnetctl read analog-input:1 present-value
```

<details>
<summary>All <code>BACNETCTL_*</code> environment variables</summary>

| Variable | Type | Description | Flag | Default |
|----------|------|-------------|------|---------|
| **BACNETCTL_ALL** | bool | **`trend`**: read the whole buffer from `--start` | `--all` | false |
| **BACNETCTL_BBMD** | string | Register as a foreign device with this BBMD, `host[:port]` | `--bbmd` | |
| **BACNETCTL_BBMD_TTL** | uint16 | Time-to-live of the foreign device registration in seconds | `--bbmd-ttl` | 60 |
| **BACNETCTL_BROADCAST** | bool | **`time sync`**: broadcast instead of sending to `--device` | `--broadcast` | false |
| **BACNETCTL_CHUNK** | uint32 | **`file get`**, **`file put`**: octets per request | `--chunk` | 256 |
| **BACNETCTL_COMM_DURATION** | uint16 | **`comm-control`**: minutes the device stays silent (0 = until enabled) | `--duration` | 0 |
| **BACNETCTL_CONFIRMED** | bool | **`monitor`**: ask for confirmed notifications | `--confirmed` | false |
| **BACNETCTL_COUNT** | uint32 | **`monitor`**, **`events`**: stop after this many notifications (0 = until interrupted); **`trend`**: records to read | `--count` | 0 (trend: 20) |
| **BACNETCTL_DEVICE** | string | Device address `host[:port]`; the router with `--network` and `--mac`; the BBMD for **`bbmd`** | `--device` | |
| **BACNETCTL_DISCOVER_NETWORK** | string | **`discover`**, **`whohas`**: ask only this BACnet network, or `all` | `--network` | |
| **BACNETCTL_DURATION** | uint32 | **`monitor`**, **`events`**: stop after this many seconds (0 = until interrupted) | `--duration` | 0 |
| **BACNETCTL_EVENT_TIME** | string | **`ack`**: date and time stamp of the event, RFC 3339 | `--event-time` | read from the object |
| **BACNETCTL_FILE_START** | uint32 | **`file put`**: position in the file object to write at | `--start` | 0 |
| **BACNETCTL_FILTER** | string | **`types`**, **`properties`**: only names that contain this text | `--filter` | |
| **BACNETCTL_HIGH** | string | **`discover`**, **`whohas`**: highest device instance to ask | `--high` | |
| **BACNETCTL_INCREMENT** | string | **`monitor`**: COV increment for `--property` | `--increment` | the device's own |
| **BACNETCTL_INDEX** | string | **`read`**, **`write`**: array index | `--index` | |
| **BACNETCTL_INPUT** | string | **`file put`**: file whose content is written | `--input` | |
| **BACNETCTL_INTERFACE** | string | Network interface whose subnet broadcast address is used | `--interface` | chosen by the system |
| **BACNETCTL_LIFETIME** | uint32 | **`monitor`**: lifetime of the subscriptions in seconds | `--lifetime` | 300 |
| **BACNETCTL_LIMIT** | uint32 | **`objects`**: list at most this many objects (0 = all) | `--limit` | 0 |
| **BACNETCTL_LOCAL_PORT** | uint16 | Local UDP port to bind (0 = a free port) | `--local-port` | 0 (`discover`, `whohas`, `events`, `router list`: 47808) |
| **BACNETCTL_LOW** | string | **`discover`**, **`whohas`**: lowest device instance to ask | `--low` | |
| **BACNETCTL_MAC** | string | MAC address of a device behind a router, in hex | `--mac` | |
| **BACNETCTL_MAX_SIZE** | uint32 | **`file get`**: refuse to read more than this many octets | `--max-size` | 16777216 |
| **BACNETCTL_NAME** | string | **`whohas`**: object name to look for | `--name` | |
| **BACNETCTL_NAMES** | bool | **`discover`**: also read the names of every device | `--names` | false |
| **BACNETCTL_NETWORK** | uint16 | BACnet network number of a device behind a router (0 = none) | `--network` | 0 |
| **BACNETCTL_OBJECT** | string | **`whohas`**: object identifier to look for | `--object` | |
| **BACNETCTL_OUTPUT** | string | **`file get`**: file to write (`-` = stdout) | `--output` | |
| **BACNETCTL_OUTPUT_FORMAT** | string | Output format: text, json or table | `--format` | text |
| **BACNETCTL_PASSWORD** | string | **`reinitialize`**, **`comm-control`**: password of the device. Read when the command runs; never shown in `--help` or in output | `--password` | |
| **BACNETCTL_POLL** | uint32 | **`monitor`**: poll every this many ms instead of subscribing (0 = subscribe) | `--poll` | 0 |
| **BACNETCTL_PRIORITY** | uint8 | **`write`**: command priority 1-16 (0 = none); **`release`**: the priority to relinquish | `--priority` | 0 |
| **BACNETCTL_PROCESS_ID** | uint32 | **`ack`**: acknowledging process identifier | `--process-id` | 0 |
| **BACNETCTL_PROPERTY** | string | **`monitor`**: subscribe to (or poll) this property; **`release`**: the property to relinquish | `--property` | (release: present-value) |
| **BACNETCTL_RETRIES** | uint8 | Retransmissions of an unanswered read | `--retries` | 3 |
| **BACNETCTL_ROUTER_NETWORK** | uint16 | **`router list`**: ask only for the router to this network (0 = all) | `--network` | 0 |
| **BACNETCTL_SEQUENCE** | string | **`ack`**: sequence number time stamp of the event | `--sequence` | read from the object |
| **BACNETCTL_SOURCE** | string | **`ack`**: who acknowledges | `--source` | bacnetctl |
| **BACNETCTL_START** | uint32 | **`trend`**: position of the first record | `--start` | 1 |
| **BACNETCTL_STATE** | string | **`ack`**: event state that is acknowledged | `--state` | the state the object is in |
| **BACNETCTL_TARGET** | string | **`discover`**, **`whohas`**, **`router list`**: ask this address instead of broadcasting | `--target` | |
| **BACNETCTL_TIME** | string | **`time sync`**: now, or an RFC 3339 time | `--time` | now |
| **BACNETCTL_TIMEOUT** | uint32 | APDU timeout in ms (0 = 3000) | `--timeout` | 3000 |
| **BACNETCTL_TYPE** | string | **`write`**: type of a value without prefix | `--type` | auto |
| **BACNETCTL_TYPES** | string | **`objects`**: only list these object types | `--type` | |
| **BACNETCTL_UTC** | bool | **`time sync`**: send UTCTimeSynchronization | `--utc` | false |
| **BACNETCTL_VALUES** | bool | **`objects`**: also read value, units, status flags and description | `--values` | false |
| **BACNETCTL_WAIT** | uint32 | **`discover`**, **`whohas`**, **`router list`**: how long to collect answers, in ms | `--wait` | 3000 |

</details>

---

## FAQ

### What is bacnetctl?

bacnetctl is a command-line client for BACnet/IP, the protocol of building automation. It finds devices, reads and writes their properties, lists their objects, watches values change, reads alarms, trend logs and files, and decodes captured frames. It is a client: it does not act as a BACnet device, and has no object of its own.

### "UDP port 47808 is in use". What now?

Another BACnet program on this computer holds the BACnet/IP port: a workstation, a gateway, a driver, another bacnetctl. Only one program can have it.

- For work with **one device** (`read`, `write`, `objects`, `monitor` and the others that take `--device`) the port does not matter. They use a free port by default, and never show this error unless you set `--local-port` yourself.
- `discover`, `whohas`, `events` and `router list` need port 47808 to hear broadcasts. Stop the other program, or do without broadcasts: `discover --target <address> --local-port 0` asks one device, which answers to the address that asked if it is a device that does so. Many answer a Who-Is by broadcast in any case: then only port 47808 hears them.

### `discover` finds nothing, but the devices are there

A Who-Is is a broadcast, and its answers are broadcasts.

- **Another IP subnet.** IP routers do not pass broadcasts on. Use `--bbmd <address>` with the BBMD of that subnet (bacnetctl registers as a foreign device), or `--target <address>` for one device you know.
- **Several network interfaces.** The broadcast leaves on the interface the operating system picks. Name the right one with `--interface`.
- **A firewall** on this computer that drops incoming UDP on port 47808.
- **Another port.** A network that does not use 47808: `--local-port 47809` binds, and broadcasts to, that port.
- **The device is slow, or the network large.** Raise `--wait`.

`--debug` shows what the client received and dropped.

### How do I reach a device in Docker, or behind NAT?

Broadcasts do not cross a port mapping. Address the device directly: `--device <host>:<mapped port>` for everything, and `discover --target <host>:<mapped port> --local-port 0` to find out what it is. A device that answers a Who-Is by broadcast inside its own network is not heard that way; `bacnetctl read --device <host>:<port> device object-identifier,object-name` always tells who is there.

A device behind NAT sees the request come from the NAT gateway and answers there, which works for requests and their answers. COV notifications (`monitor`) are sent later, to the address the subscription came from: they arrive only while the NAT mapping lives. `monitor --poll` does not depend on that.

### How do I address a device on MS/TP, or on another BACnet network?

Through its router: `--device <router address> --network <network number> --mac <MAC in hex>`. `bacnetctl discover` lists the network and MAC of every device it finds (an MS/TP station number 10 is MAC `0a`), and `bacnetctl router list` shows which router leads to which network.

### Why does a write end with "outcome unknown"?

BACnet/IP runs on UDP. When the acknowledgement of a write does not arrive, there is no telling whether the request was lost on the way there (nothing happened) or the answer on the way back (the write was carried out). bacnetctl does not guess and does not send the write again, because a second write is not always harmless. It reports `outcome unknown` (exit code 5, error code `outcome_unknown`) and leaves the decision to you: read the property back, then repeat the write if it is needed.

A **read** that gets no answer is simply sent again (`--retries`), and ends as a plain timeout.

### I wrote a value, and the automation no longer controls the point

A commandable property keeps a value for each of 16 priorities and takes the highest one. A value you write with `--priority 8` (the priority of a manual operator) stays in force, and overrides the control program at its lower priority, until priority 8 is relinquished: `bacnetctl release <object> --priority 8`. Writing another value at priority 8 only replaces your override with another. `bacnetctl read <object> priority-array` shows what is commanded at which priority.

Without `--priority` a device takes the lowest, 16, which a control program normally overrides again at once.

### The device refuses my write

- `write-access-denied`: the property is read-only, or the device protects it. The present value of an input can only be written while `out-of-service` is true.
- `invalid-data-type`: the value has the wrong type. Let bacnetctl look it up (no prefix), or read the property first to see what it holds.
- `value-out-of-range`: the value is outside what the object accepts.
- `unknown-object`, `unknown-property`: check the spelling against `bacnetctl objects` and `bacnetctl describe`.

### Texts show up as garbage

BACnet character strings come in several character sets. bacnetctl converts UTF-8, UCS-2, UCS-4 and ISO 8859-1 (Latin-1) to UTF-8. The two legacy double-byte sets (IBM/Microsoft DBCS and JIS X 0208) are not converted: their octets are shown as they are. Strings that `write` sends are UTF-8, which some older devices refuse (`character-set-not-supported`).

### What do the numbers after a name mean?

`active (1)` and `degrees-celsius (62)` are enumerated values: the name bacnetctl knows for the number, and the number that is on the wire. A number without a name is an enumeration bacnetctl has no table for (a vendor's own, or a property it does not know). `{in-alarm, fault}` is a bit string as the names of the bits that are set, `{}` one with none set.

### What has it been tested with?

Two things, neither of them a physical controller.

**The simulated device of its own test suite** (`internal/bactest`), an in-process BACnet/IP device built from the codecs of go-bacnet. Every command is run against it automatically, over the loopback interface, in all three output formats. That proves the commands, their output and their exit codes; since client and simulated device share the same codecs, it does not prove interoperability.

**Five open-source stacks in containers**, by hand, before 0.1.0, with go-bacnet v0.4.0: bacnet-stack 1.6.1, BACpypes3 0.0.110, BACnet4J 6.1.1, Worldiety and System.IO.BACnet 4.0.0 (the stack behind YABE), plus bacnet-stack's own router, as published by [bacnet-interop](https://github.com/otfabric/bacnet-interop) v0.10.0. What was run:

| Command | Against | Result |
|---|---|---|
| `discover` by broadcast | all five | every stack answers |
| `discover --bbmd` | BACnet4J as BBMD | registers as a foreign device and finds the devices behind it; `bbmd fdt` then lists the registration |
| `discover --network`, `router list` | bacnet-stack router | finds the route and the device behind it |
| `whohas --name` | all five | answered |
| `device`, `objects --values` | all five | BACpypes3 and Worldiety do not answer `device:4194303`; the instance is then learned with a Who-Is |
| `read` (one and several properties, `priority-array`), `describe` | bacnet-stack, BACpypes3, BACnet4J, System.IO.BACnet | |
| `read`, `objects`, `write` with `--network/--mac` | a bacnet-stack device behind the bacnet-stack router | |
| `write --priority`, `release` | BACnet4J, System.IO.BACnet | value, priority array and fallback read back |
| `write` with several properties | all five | all written; a refused write reported per row on bacnet-stack, BACnet4J and Worldiety (exit code 7); BACpypes3 refuses without naming the write (exit code 6) |
| `create`, `delete` | all five | bacnet-stack creates an analog value with a given instance and with one of its own choice, lists it and deletes it again; it refuses an initial object name (write-access-denied). BACnet4J answers dynamic-creation-not-supported and object-deletion-not-permitted; BACpypes3, System.IO.BACnet and Worldiety do not implement the services (Reject) |
| `monitor` | bacnet-stack, BACpypes3, BACnet4J, System.IO.BACnet | initial value and the change after a write; `--property --increment --confirmed` on BACnet4J; `--poll` on Worldiety, which has no COV |
| `trend` | bacnet-stack, BACnet4J, System.IO.BACnet | |
| `alarms`, `ack` | bacnet-stack, BACnet4J | an out-of-range alarm raised with a write, listed, acknowledged |
| `file get`, `file put` | bacnet-stack, BACnet4J | read, overwritten, read back with matching SHA-256 |
| `time sync`, `time sync --utc` | bacnet-stack, BACnet4J | sent; bacnet-stack logged the request |
| `bbmd bdt`, `bbmd fdt` | bacnet-stack, BACnet4J | |
| `reinitialize warmstart` | BACnet4J | acknowledged |
| `comm-control disable` / `enable` | BACnet4J | the device stops answering and answers again; bacnet-stack refuses "disable" (service-request-denied) |
| refusals and timeouts | bacnet-stack, BACnet4J | unknown object, write-access-denied, no answer |

Not run against any stack: `events` (no stack in that set sends an event notification to a listener it was not told about). It is tested against the simulated device only. The library underneath, [go-bacnet](https://github.com/otfabric/go-bacnet), tests event notifications and everything above automatically against the same stacks and documents the results in its `INTEROP.md`.

In that session all devices had the same device instance, which a real network must not have: the library keeps at most eight addresses per instance, so `discover` listed eight of ten devices.

**bacnetctl has not been run against physical devices.**

If you use bacnetctl with a device it has trouble with, please [open an issue](https://github.com/otfabric/bacnetctl/issues) with the `--debug` output and, if you can, the frames as `bacnetctl decode` shows them.

### Does it support BACnet/SC, BACnet/IPv6 or MS/TP?

No. bacnetctl speaks BACnet/IP over IPv4. MS/TP devices are reached through a BACnet router, like any device on another BACnet network. BACnet Secure Connect and BACnet/IPv6 are not supported.

### How do I use bacnetctl in scripts and CI?

Pass `--format json` (or set `BACNETCTL_OUTPUT_FORMAT=json`). Progress goes to stderr, the result goes to stdout, and a failure returns one JSON error object with a stable exit code, whatever went wrong and however early. `bacnetctl version --format json` tells a script which build it runs. `bacnetctl read … present-value` in text format prints the bare value. See [General CLI usage](#general-cli-usage), [docs/json-output.md](docs/json-output.md) and [docs/exitcodes.md](docs/exitcodes.md).

---

## Project structure

<details>
<summary>Repository layout</summary>

```console
bacnetctl/
├── cmd/ (root command and every command: discover, read, write, monitor, …, version, completion)
├── internal/
│   ├── config (CLI & env: configuration types, flag and environment binding, device addresses)
│   ├── cli (shared Cobra helpers: enum completion, debug)
│   ├── format (stdout **text/json/table** via `Write`, streaming output via `StreamWriter`, table rendering)
│   ├── runner (command wiring, formatted and streaming runs, fatal JSON/text rendering)
│   ├── errs (typed CLI errors, stable codes, exit-code mapping)
│   ├── bac (what the commands do: the BACnet/IP session, addressing, names, value parsing and rendering, the services, error classification, frame decoding)
│   ├── bactest (the simulated BACnet/IP device of the tests; bactest/bacsim runs it stand-alone)
│   └── types (DTOs for JSON/text/table output)
├── docs/ (exit codes, JSON output contract)
├── Makefile (Build & test commands)
└── go.mod (Dependencies)
```

</details>

## Development

A toplevel `Makefile` is provided for convenience. Run **`make`** (or **`make help`**) for the full list. Common targets:

| Target | Purpose |
|--------|---------|
| **`make test`** | Unit and end-to-end tests with race detector (`go test -race ./...`) |
| **`make coverage`** | Tests + **`coverage.out`** across the whole module; fails below **85%** total statement coverage (`COVERAGE_MIN`) |
| **`make compile`** | Full-module typecheck (`go build ./...`) |
| **`make lint`** | `compile` then **staticcheck** |
| **`make lint-ci`** | `compile`, **go vet**, **golangci-lint** (uses `.golangci.yml`) |
| **`make fmt`** / **`fmt-check`** | Format with the gofmt of the Go version in `go.mod` (the one CI uses); `fmt-check` fails if that gofmt or the locally installed one would change any file |
| **`make check`** | `fmt`, `fmt-check`, `lint`, `lint-ci`, `vet`, `test`, **`coverage`** |
| **`make build`** | Runs **`check`** then writes **`bin/bacnetctl`** |
| **`make build-all`** / **`release-all`** | Cross-compile and package archives |

### Building

`make build` writes the binary to **`bin/bacnetctl`** (not the repo root).

```console
make build
make build-all
```

### Running Tests

```console
make test
make coverage
```

Tests need no hardware and no network beyond the loopback interface: `internal/bactest` is a simulated BACnet/IP device on a free UDP port, and the tests of every command run the real command tree against it. To try bacnetctl by hand against the same device:

```console
go run ./internal/bactest/bacsim -listen 127.0.0.1:47809 &
bacnetctl objects --device 127.0.0.1:47809 --values
```

## Version History

Release notes for each version are maintained in **[RELEASE.md](RELEASE.md)**.

---

## Contributing

Bug reports, device-compatibility notes and pull requests are welcome. Please [open an issue](https://github.com/otfabric/bacnetctl/issues) to discuss larger changes first, and run `make check` before submitting. See [CONTRIBUTING.md](CONTRIBUTING.md) for the development setup and guidelines.

Please report security vulnerabilities privately, as described in [SECURITY.md](SECURITY.md), not in a public issue.

If bacnetctl is useful to you, a star on GitHub helps other engineers find it.

---

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE).
