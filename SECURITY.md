# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in `bacnetctl`, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, please email **security@otfabric.io**, or open a private advisory on GitHub (the [Security](https://github.com/otfabric/bacnetctl/security) tab → **Advisories** → **Report a vulnerability**), with:

- A description of the vulnerability
- Steps to reproduce the issue
- The affected version(s) (`bacnetctl version` output)
- Any potential impact you've identified

We will acknowledge your report within 48 hours and aim to provide a fix or mitigation within 7 days for critical issues.

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest release | Yes |
| Older releases | Best effort |

## Security Considerations

### BACnet/IP Has No Built-In Security

BACnet/IP carries no authentication and no encryption: anyone who can reach a device on UDP port 47808 can read it, write to it and restart it, and anyone on the path can read or alter the traffic. `bacnetctl` does not change that, and does not implement BACnet Secure Connect (BACnet/SC).

- **Use it only on devices and networks you own or are authorized to work on**
- **Keep building automation traffic on isolated, trusted networks**; never expose UDP port 47808 to the Internet
- A device has no way to tell bacnetctl from its own management system: what you send is carried out

### Commands That Change a Device

- **`write`** changes a property. Written to a present value it starts and stops equipment, moves valves and dampers and changes setpoints; with `--priority` it overrides the control program until that priority is relinquished. Know what an object controls before writing to it. Everything is validated before anything is sent, and a write is sent exactly once (never retried)
- **`release`** relinquishes a command priority: the object takes the value of the next priority, or its relinquish default
- **`ack`** acknowledges an alarm: the device records it, with the source you give, and stops presenting the alarm as unacknowledged
- **`time sync`** changes the clock of a device, or with `--broadcast` of every device that hears it. Schedules, trend logs and alarm time stamps follow the clock
- **`file put`** writes a file object, which can hold a configuration or a program the device acts on
- **`reinitialize`** restarts a device, or runs a step of its backup and restore procedure. While a controller restarts it controls nothing
- **`comm-control`** silences a device: it drops out of the management system, and its alarms are not delivered

`reinitialize` and `comm-control` refuse to run without `--yes-i-am-sure` and send nothing then. That flag has no environment variable.

When no acknowledgement arrives for one of these requests, the device may or may not have carried it out. bacnetctl reports that as "outcome unknown" (exit code 5) and never sends the request a second time.

### Passwords Travel in Clear Text

A device can ask for a password in ReinitializeDevice and DeviceCommunicationControl. BACnet sends it as plain text in the request: anyone who can capture the traffic can read it. Treat such a password as a guard against accidents, not as a secret.

- Give it with `--password`, or with the environment variable `BACNETCTL_PASSWORD`, which keeps it out of the shell history and the process list
- bacnetctl never prints the password: not in results, not in errors, not with `--debug`, and not in `--help` (the variable is read when the command runs, not shown as the default of the flag)
- `decode` does not show the password of a decoded device management request either; the hex you give it still contains it

### Commands That Are Read-Only but Not Invisible

- **`discover`**, **`whohas`** and **`router list`** broadcast. Every device on the network, and on the networks behind its routers and BBMDs, receives the request, and many answer. On a large or fragile network (MS/TP segments behind routers) a global Who-Is causes a burst of traffic: limit it with `--low` / `--high`, `--network` or `--target`
- **`--bbmd`** registers this computer as a foreign device with a BBMD. The registration appears in the BBMD's foreign device table and lapses with its time-to-live
- **`monitor`** creates subscriptions on a device, which has a limited number of them. bacnetctl cancels its subscriptions when it ends; if it is killed, they lapse with their lifetime (`--lifetime`)
- **`objects`**, **`describe`** and **`trend --all`** send many requests. A small controller serves them at the expense of its other communication

### Listening

- **`discover`**, **`whohas`**, **`events`** and **`router list`** bind UDP port 47808 on **all network interfaces** for as long as they run, and accept datagrams from any address, as BACnet requires. bacnetctl is not a BACnet device and serves no requests: it acknowledges a confirmed notification that is addressed to it, and rejects any other confirmed request
- The other commands bind a free UDP port for the duration of one invocation

### Output

- Command output contains device addresses, object names, process values and network topology, which may be sensitive operational data. Be careful when storing or sharing it
- `file get` writes what the device sends to the path you name, and overwrites an existing file. `--max-size` bounds it
- `--debug` logs the diagnostics of the BACnet client to stderr: addresses and message kinds, no payloads

### Privileges

`bacnetctl` needs no elevated privileges: UDP port 47808 is above 1024. A host firewall may have to allow incoming UDP on that port for `discover` and `events`.

### Verifying Downloads

Release archives are published with SHA-256 checksums. Verify them as shown in the [installation instructions](README.md#installation) before installing.
