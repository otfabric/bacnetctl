# bacnetctl Releases

## v0.2.1

**Date:** unreleased  
**Previous release:** v0.2.0

### Summary

`monitor` no longer hides that notifications were lost.

### Fixes

- **`monitor`:** when notifications were lost because output could not keep up, and a subscription renewal came before the next notification, that notification was printed without the `gap` field (text: "notifications were lost before this one"). It now carries the mark.

### Documentation

- `docs/json-output.md`: `timestamp` is when a record is printed, not when it arrived, and the order of records holds per object only. The output itself did not change.

---

## v0.2.0

**Date:** 2026-10-30
**Previous release:** v0.1.0

### Summary

**Breaking for scripts:** the exit codes follow the OT Fabric organisation table (ADR-0006 of otfabric/engineering). Nothing else changes in the commands.

### Exit codes

| Case | Old | New | JSON error |
|---|---|---|---|
| `context_canceled` (interrupted before a result) | 10 | 130 | kind `interrupted`, was `internal` |
| `unsupported` | 6 | 8 | kind `unsupported`, was `protocol` |

Every other exit code is unchanged. See [docs/exitcodes.md](docs/exitcodes.md).

### Environment

`BACNETCTL_FORMAT` is now the canonical name of the format variable; `BACNETCTL_OUTPUT_FORMAT` stays accepted, and `BACNETCTL_FORMAT` wins when both are set.

---

## v0.1.0

**Date:** unreleased  
**Previous release:** none

### Summary

First release. bacnetctl is a command-line client for **BACnet/IP**: it finds devices, reads and writes their properties, lists their objects, watches values change, reads alarms, trend logs and files, shows routers and BBMD tables, and decodes captured frames. Built on [go-bacnet](https://github.com/otfabric/go-bacnet) v0.4.0.

### Highlights

- **`discover`:** Who-Is / I-Am by broadcast, to one address (**`--target`**), through a BBMD as a foreign device (**`--bbmd`**) or to one BACnet network behind a router (**`--network`**); **`--low`** / **`--high`** for a range of device instances, **`--names`** for the names of the devices.
- **`whohas`:** Who-Has / I-Have by object name (**`--name`**) or identifier (**`--object`**).
- **`read`:** One property with ReadProperty, several properties of several objects with ReadPropertyMultiple, split to the device's APDU size. Objects and properties by name, short form or number; array indexes; the special properties **`all`**, **`required`** and **`optional`**. A property the device refuses is reported in its row and the others are still read (exit code **7**). Devices without ReadPropertyMultiple are read one property at a time.
- **`objects`:** The object list with names and, with **`--values`**, present values with units, status flags and descriptions; **`--type`**, **`--limit`**.
- **`describe`** and **`device`:** Every property of one object, decoded; identity and capabilities of a device, with the services and object types it supports by name.
- **`write`** with several properties: one WritePropertyMultiple request for any number of `OBJECT PROPERTY VALUE` groups. The device stops at the first write it refuses; the result reports every write as written, failed (with the error of the device) or not-written, with exit code **7**.
- **`create`** and **`delete`:** CreateObject with an instance of your choice or the device's and typed initial values (**`--name`**, `PROPERTY=VALUE`); DeleteObject, only with an explicit **`--yes-i-am-sure`**. Both are sent exactly once.
- **`write`** and **`release`:** WriteProperty with typed values (**`real:`**, **`unsigned:`**, **`signed:`**, **`bool:`**, **`enum:`**, **`string:`**, **`double:`**, **`octets:`**, **`bits:`**, **`date:`**, **`time:`**, **`object:`**, **`null`**), or the type looked up from the device; **`--priority`**, **`--index`**. Everything is validated before anything is sent, and a write is sent exactly once. A write that is not acknowledged is reported as **outcome unknown** (exit code **5**, code `outcome_unknown`), never as success or failure.
- **`monitor`:** SubscribeCOV, or SubscribeCOVProperty with **`--property`** and **`--increment`**; **`--confirmed`**, **`--lifetime`**, **`--count`**, **`--duration`**. Subscriptions are renewed while monitoring runs and cancelled on the device when it ends. **`--poll`** reads at an interval instead, for devices without COV.
- **`alarms`**, **`ack`**, **`events`:** GetEventInformation with GetAlarmSummary as fallback; AcknowledgeAlarm with the state and time stamp read from the object; a listener for event notifications.
- **`trend`:** Trend log records with ReadRange by position: **`--start`**, **`--count`**, **`--all`**.
- **`file get`** and **`file put`:** AtomicReadFile and AtomicWriteFile for stream-access files, with the SHA-256 of the content.
- **`time sync`:** TimeSynchronization or UTCTimeSynchronization (**`--utc`**), to one device or by broadcast.
- **`router list`**, **`bbmd bdt`**, **`bbmd fdt`:** Who-Is-Router-To-Network; the broadcast distribution table and the foreign device table of a BBMD. Devices behind a router are addressed with **`--network`** and **`--mac`** in every device command.
- **`reinitialize`** and **`comm-control`:** ReinitializeDevice and DeviceCommunicationControl, only with an explicit **`--yes-i-am-sure`**. The password (**`--password`** or `BACNETCTL_PASSWORD`) is never printed.
- **`decode`:** Offline decoding of a BACnet/IP datagram from hex: BVLC, NPDU, APDU and the service with its parameters.
- **`types`** and **`properties`:** The object types and property identifiers bacnetctl knows by name.
- **Addressing:** **`--device host[:port]`**; **`--local-port`** (a free port for work with one device, 47808 for the commands that need broadcasts), **`--interface`**, **`--bbmd`**, **`--timeout`**, **`--retries`**.
- **Output:** **`--format text|json|table`** on every command; streams write JSON Lines. Values with names for enumerations, units and bit strings; character strings in UTF-8, UCS-2, UCS-4 and ISO 8859-1. A single JSON error object on failure with `--format json`, carrying the error class and code of the device, also for an invocation that is refused before a command runs (an unknown flag, a missing argument); stable exit codes (0, 2, 3, 4, 5, 6, 7, 10). **`version --format json`** for scripts. The JSON shapes and exit codes are documented in **`docs/json-output.md`** and **`docs/exitcodes.md`**.
- **Environment:** Every configuration flag has a **`BACNETCTL_*`** variable; a flag wins over the environment.
- **`--debug`:** Logs the diagnostics of the BACnet client and shows the causes of an error.
- **Completion:** bash, zsh, fish and PowerShell; object types, property names and the values of flags.

### Limitations

- BACnet/IP over IPv4 only: no BACnet/SC, no BACnet/IPv6, no MS/TP data link (MS/TP devices are reached through a router).
- Client only: bacnetctl is not a BACnet device and does not answer Who-Is.
- No list element services (AddListElement, RemoveListElement), no text messages, no private transfer, and no SubscribeCOVPropertyMultiple: `monitor` subscribes per object.
- `write` with several properties is not turned into single writes for a device without WritePropertyMultiple.
- Files: stream access only. Trend logs: by position only, not by time or sequence number.
- `time sync` cannot address one device behind a router.
- The name tables cover the standard object types and the common standard properties; others are written by number. Vendor-proprietary enumerations are shown as numbers.
- Character strings in the DBCS and JIS X 0208 character sets are not converted.

### Quality

- **Tests:** Whole-module statement coverage of **93%**, with a floor of 85% enforced by **`make coverage`** (`COVERAGE_MIN`). Every command is tested through the real command tree against an in-process simulated BACnet/IP device (**`internal/bactest`**), in all three output formats, with its exit codes.
- **Interoperability:** Run by hand against bacnet-stack 1.6.1, BACpypes3 0.0.110, BACnet4J 6.1.1, Worldiety, System.IO.BACnet 4.0.0 and the bacnet-stack router (bacnet-interop v0.10.0): every command except `events`, which no stack in that set can drive (`create` and `delete` succeed on bacnet-stack only: the other four refuse or do not implement them); the README lists which command ran against which stack. Not run against physical devices.
- **Hardware-free:** The test suite needs no device and no network beyond the loopback interface.

---
