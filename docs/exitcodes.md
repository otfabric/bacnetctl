# Process exit codes

`bacnetctl` uses stable exit codes for scripting. A fatal error is rendered once: as one `error:` line on stderr, or as a single JSON `error` object on stdout when `--format json` is active. JSON is active when the command line has `--format json` or `BACNETCTL_FORMAT` says so (the flag wins), and that is honoured for usage errors too: an unknown flag or command, a wrong number of arguments or a malformed environment variable is answered with the JSON object, although no command ran.

| Code | Meaning |
|------|---------|
| 0 | Success (`ExitOK`) |
| 2 | Usage (`KindUsage`): unknown command or flag, wrong number of arguments, a value a flag cannot hold, a device management command or a `delete` without `--yes-i-am-sure` |
| 3 | Invalid input (`KindInvalidInput`): an object, property, value, address, combination of flags or environment variable that does not make sense. Nothing was sent |
| 4 | Transport (`KindTransport`): the local UDP port is in use or cannot be opened, a host name does not resolve, a datagram cannot be sent, a BBMD does not accept the foreign device registration |
| 5 | Timeout (`KindTimeout`): the device did not answer in time. For a request that changes the device (a write, an acknowledgement, a restart) this means the **outcome is unknown**: code `outcome_unknown` |
| 6 | Device or protocol (`KindDevice`, `KindProtocol`): the device refused the request with an Error, a Reject or an Abort, or what it sent violates the protocol |
| 7 | Partial success (`ExitPartial`): a **valid** result was written to stdout, and it holds failures: a property of `read` or `describe` the device refused, a write of a `write` with several properties the device refused (those before it are written, those after it are not), an object of `objects` whose name could not be read, a device of `discover --names` that did not give its names |
| 8 | Unsupported (`KindUnsupported`): the operation is not supported: code `unsupported` (an optional service the device does not provide, such as ReadPropertyMultiple for `required` and `optional`) |
| 10 | Output / internal (`KindOutput`, `KindInternal`): the result could not be written, or something unexpected happened |
| 130 | Interrupted (`KindInterrupted`): Ctrl-C or SIGTERM before a one-shot command had its result |

**Kind vs code:** `kind` drives the exit code; `code` in the JSON error object is stable for machines. Do not parse the human `message`.

| `kind` | `code` values |
|--------|---------------|
| `usage` | `invalid_input`, `confirmation_required` |
| `invalid_input` | `invalid_input`, `invalid_format`, `invalid_address`, `invalid_config`, `input_file_open_failed`, `input_decode_failed` |
| `transport` | `transport_port_in_use`, `transport_bind_failed`, `transport_resolve_failed`, `transport_send_failed`, `foreign_device_registration_failed`, `connection_lost` |
| `timeout` | `request_timeout` (no answer to a request), `outcome_unknown` (a request that changes the device was sent and not acknowledged) |
| `device_error` | for an **Error**: the BACnet error code with underscores, such as `unknown_object`, `unknown_property`, `write_access_denied`, `value_out_of_range`, `invalid_data_type`, `invalid_array_index`, `password_failure`; for a **Reject**: `reject_` and the reason, such as `reject_unrecognized_service`; for an **Abort**: `abort_` and the reason, such as `abort_segmentation_not_supported` |
| `protocol` | `protocol_violation`, `malformed_response`, `apdu_too_large`, `no_route`, `bvlc_result` (a BBMD refused a request) |
| `output` | `json_encode_failed`, `table_render_failed`, `output_write_failed`, `output_file_write_failed` |
| `unsupported` | `unsupported` |
| `internal` | `internal_unexpected` |
| `interrupted` | `context_canceled` |

The code of a `device_error` is not a closed list: it follows what the device answers. A code or reason go-bacnet has no name for is written `code_N` or `reason_N`. The error object carries the device's answer in full under `bacnet`: see [json-output.md](json-output.md).

**Outcome unknown:** BACnet/IP runs on UDP. When the acknowledgement of `write`, `release`, `create`, `delete`, `ack`, `file put`, `reinitialize` or `comm-control` does not arrive, the request may or may not have reached the device. bacnetctl never sends such a request a second time. It exits `5` with code `outcome_unknown`; read the state of the device before you repeat the command. A read that is not answered is retransmitted (`--retries`) and ends with `request_timeout`.

**Several writes:** `write` with more than one property sends one WritePropertyMultiple. A device refuses it as a whole (`6`: it does not implement the service, or it does not say which write failed) or names the first write it refused: then the result is written with a status per write and the exit code is `7`.

**Partial exit:** only after a success payload was written. A failure to open the socket, to validate or to write is fatal and uses the codes above, never `7`. With **one** property, `read` uses ReadProperty and a refusal of the device is fatal (`6`); with several, the refusal is in the row of that property (`7`).

**Nothing found:** `discover`, `whohas` and `router list` exit `0` with an empty result when nothing answers. A device that does not answer a request addressed to it is a timeout (`5`).

**Streaming commands:** `monitor` and `events` write one record at a time. Stopping them with Ctrl-C (SIGINT) or SIGTERM ends the stream normally and does not change the exit code: they exit `0` when they end by `--count`, `--duration` or a signal. A subscription the device refuses ends `monitor` before it starts, with the fatal code of that error.

**Commands that change a device:** `write`, `release`, `create`, `delete`, `ack`, `time sync`, `file put`, `reinitialize` and `comm-control` validate everything before sending; a rejected invocation exits `2` or `3` and has sent nothing. A refusal by the device exits `6`, a missing answer `5` (outcome unknown). `time sync` is an unconfirmed service: it exits `0` once the request is sent.

**`--debug`:** adds the diagnostics of the BACnet client and, after a fatal error, the chain of causes (`cause: …` lines) on stderr. It does not change stdout or the exit code.

**Organisation table:** these codes follow the OT Fabric CLI exit-code table. bacnetctl does not use `1` and `9`: they keep their organisation meaning and are not reused for anything else.

**Format variable:** `BACNETCTL_FORMAT` is the name to use; `BACNETCTL_OUTPUT_FORMAT` stays accepted. When both are set, `BACNETCTL_FORMAT` wins.
