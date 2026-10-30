# `--format json` field conventions

These conventions are a **stable contract** for scripting. Field names are snake_case. A breaking change to a name or shape is called out in the release notes.

## General

| Concept | JSON fields |
|--------|-------------|
| Device address | `target`: `host:port` of the device, followed by ` net N mac HEX` for a device behind a router |
| Object | `object` (name, `"analog-input:1"`), `object_type` (number, `0`) and `instance` (number, `1`). An object type without a name is its number: `"300:1"` |
| Property | `property` (name, `"present-value"`), `property_id` (number, `85`) and, when an array index was used, `index`. A property without a name is its number as a string |
| Value | a **Value** object, below |
| Enumerations in results | the name and the number side by side: `event_state` / `event_state_id`, `segmentation` / `segmentation_id`, `units` / `units_id` |
| Times | `timestamp`: when bacnetctl printed the record, RFC 3339 with milliseconds, UTC. That is not the time of arrival: a notification that waited in the queue is stamped later, and the order of records holds per object only. Time stamps **of the device** (`event_time`, `time_stamps`, the `timestamp` of a trend log record, `local_date`, `local_time`) are text as the device sent them: its local time, without a zone |
| Durations | `duration_ms`, `rtt_ms` (numbers, milliseconds with fractions) |
| Repeated rows | `devices`, `objects`, `rows`, `properties`, `alarms`, `records`, `routers`, `entries`, `names`: **arrays of objects**, empty arrays rather than null |
| A row that failed | `error` object (`kind`, `code`, `message`, `bacnet`); omitted when the row is fine |
| Fatal error | one object `{"error": {"kind", "code", "message", "bacnet"}}` on stdout, nothing else, also when the invocation itself is wrong (an unknown flag, a missing argument) and JSON was asked for with `--format json` or `BACNETCTL_FORMAT=json`; see [exitcodes.md](exitcodes.md) |

## Value

One BACnet value. Used wherever a property value appears.

| Field | Meaning |
|-------|---------|
| `type` | `null`, `boolean`, `unsigned`, `signed`, `real`, `double`, `octet-string`, `character-string`, `bit-string`, `enumerated`, `date`, `time`, `object-identifier`, `list`, `time-stamp` or `context` |
| `value` | a native JSON type: `null`; a boolean; a number for `unsigned`, `signed`, `real`, `double` and `enumerated`; a string for the others (below); an array of **Value** objects for `list` |
| `name` | `enumerated`: the name of the value, where bacnetctl knows the enumeration of the property (`"active"`, `"degrees-celsius"`, `"high-limit"`) |
| `bits` | `bit-string`: the names of the bits that are set, where bacnetctl knows the bits of the property (`status-flags`, `event-enable`, `acked-transitions`, `limit-enable`, `protocol-services-supported`, `protocol-object-types-supported`). An empty array means that no bit is set; the field is absent when the bits have no names |
| `unit` | the symbol of the engineering unit (`"°C"`, `"%RH"`, `"kW"`), on a present value that was read together with `units` |
| `object_type`, `instance` | `object-identifier`: the numbers of the identifier |
| `tag` | the context tag number of an element of a constructed value |

Strings: `character-string` is the text in UTF-8; `octet-string` and `context` are hex (`"0a1b2c"`); `bit-string` is ones and zeros, first bit first (`"1011"`); `date` is `YYYY-MM-DD` with `*` for an unspecified field; `time` is `HH:MM:SS.hh`; `object-identifier` is `type:instance`; `time-stamp` is a time, `"sequence N"` or a date and time.

A `real` or `double` that is not a number (`NaN`, `+Inf`, `-Inf`) is written as that string, since JSON has no such numbers.

A list (an array or list property, or a constructed value) is `type` `list` with its elements as `value`. An element under a context tag has `tag`; when it is a primitive its type is not on the wire, and it is `type` `context` with its octets as hex.

```json
{"type": "real", "value": 21.5, "unit": "°C"}
{"type": "enumerated", "value": 1, "name": "active"}
{"type": "bit-string", "value": "1000", "bits": ["in-alarm"]}
{"type": "object-identifier", "value": "analog-input:1", "object_type": 0, "instance": 1}
{"type": "list", "value": [{"type": "null", "value": null}, {"type": "real", "value": 22.5}]}
```

## Error object

Used for a fatal error (`{"error": {…}}`) and for a row that failed.

| Field | Meaning |
|-------|---------|
| `kind` | `usage`, `invalid_input`, `transport`, `timeout`, `device_error`, `protocol`, `unsupported`, `output`, `internal` or `interrupted` |
| `code` | stable code; see [exitcodes.md](exitcodes.md) |
| `message` | text for people; do not parse it. In a row it is short: the name of the BACnet error |
| `bacnet` | what the device answered, for `device_error`: `pdu` (`"error"`, `"reject"` or `"abort"`); for an error `class`, `class_id`, `code`, `code_id` (`"property"`, `2`, `"unknown-property"`, `32`); for a reject or an abort `reason`, `reason_id` |

```json
{
  "error": {
    "kind": "device_error",
    "code": "unknown_object",
    "message": "the device refused the request: unknown-object (error class object)",
    "bacnet": {"pdu": "error", "class": "object", "class_id": 1, "code": "unknown-object", "code_id": 31}
  }
}
```

## Results

| Command | Shape |
|---------|-------|
| `discover` | `mode` (`"broadcast"` or `"unicast"`), `target` (with `--target`), `duration_ms`, `count`, `devices`: `instance`, `address` (where the I-Am came from: the device, or its router), `network` and `mac` (a device behind a router), `vendor_id`, `max_apdu`, `segmentation`, `segmentation_id`; with `--names` also `name`, `vendor_name`, `model_name`, or `error` |
| `whohas` | `mode`, `target`, `query`, `duration_ms`, `count`, `objects`: the **object** fields, `name`, `device` (instance), `address`, `network`, `mac` |
| `read` | `target`, `service` (`"read-property"` or `"read-property-multiple"`), `count`, `failed`, `rows`: the **object** and **property** fields and either `value` or `error` |
| `describe` | `target`, the **object** fields, `name`, `count`, `failed`, `properties`: rows as of `read` |
| `objects` | `target`, `device`, `total` (objects of the device), `count` (objects listed), `failed`, `objects`: the **object** fields, `name`, and with `--values` `present_value` (a **Value**), `units`, `units_id`, `status_flags` (the flags that are set; empty when none is), `description`; `error` when the name could not be read. A property the object does not have is omitted |
| `device` | `target`, the **object** fields of the Device object, `name`, `description`, `location`, `vendor_name`, `vendor_id`, `model_name`, `serial_number`, `firmware_revision`, `application_software_version`, `system_status`, `system_status_id`, `protocol_version`, `protocol_revision`, `max_apdu`, `segmentation`, `segmentation_id`, `max_segments`, `apdu_timeout_ms`, `apdu_retries`, `database_revision`, `local_date`, `local_time`, `utc_offset_minutes`, `object_count`, `services_supported`, `object_types_supported` (arrays of names). A property the device does not have is omitted |
| `write` (one property), `release` | `target`, the **object** and **property** fields, `value` (what was written), `priority` (when given), `status` (`"written"` or `"relinquished"`), `rtt_ms` |
| `write` (several properties) | `target`, `service` (`"write-property-multiple"`), `count`, `failed` (0 or 1: the device stops at the first write it refuses), `not_written` (the writes after that one), `rtt_ms`, `writes`: the **object** and **property** fields, `value`, `priority` (when given), `status` (`"written"`, `"relinquished"`, `"failed"` or `"not-written"`) and, on the failed one, `error`. The shape follows the number of `OBJECT PROPERTY VALUE` groups on the command line, not the outcome |
| `create`, `delete` | `target`, the **object** fields (of `create`: the identifier the device answered, which holds the instance it chose), `status` (`"created"` or `"deleted"`), `rtt_ms` |
| `monitor` (stream) | one object per notification (JSON Lines): `timestamp`, `source` (`"cov"` or `"poll"`), `device` (the device that sent it; COV only), the **object** fields, `time_remaining` (seconds the subscription has left; COV only), `values`: the **property** fields and `value`; `gap` when notifications before this one were lost |
| `alarms` | `target`, `service` (`"get-event-information"` or `"get-alarm-summary"`), `count`, `alarms`: the **object** fields, `event_state`, `event_state_id`, `unacknowledged` (transitions: `"to-offnormal"`, `"to-fault"`, `"to-normal"`), and with GetEventInformation `notify_type`, `notify_type_id`, `event_enable`, `time_stamps` (three, as text), `priorities` (three numbers) |
| `ack` | `target`, the **object** fields, `state`, `state_id`, `time_stamp` (of the event that was acknowledged), `source`, `status` (`"acknowledged"`), `rtt_ms` |
| `events` (stream) | one object per notification (JSON Lines): `timestamp`, `from` (address), `confirmed`, `device`, the **object** fields, `event_type`, `event_type_id`, `notify_type`, `notify_type_id`, `from_state`, `to_state`, `to_state_id`, `priority`, `notification_class`, `ack_required`, `message`, `event_time` (what `ack` needs), `gap` |
| `trend` | `target`, the **object** fields, `total` (records in the buffer, when the device says), `start`, `count`, `partial` (more records follow), `records`: `position`, `timestamp`, `kind` (`"value"`, `"status"`, `"failure"`, `"time-change"`), `value` (a **Value**), `status_flags` |
| `file get`, `file put` | `target`, `direction` (`"download"` or `"upload"`), the **object** fields, `length`, `sha256`, `path`, `requests` (upload), `duration_ms` |
| `time sync` | `target` (the device, or `"broadcast"`), `service` (`"time-synchronization"` or `"utc-time-synchronization"`), `time` (as sent), `status` (`"sent"`) |
| `router list` | `mode`, `target`, `duration_ms`, `count`, `routers`: `network`, `router` (address), `state` (`"available"`, `"busy"`, `"rejected"`, `"expired"`) |
| `bbmd bdt`, `bbmd fdt` | `bbmd`, `table` (`"bdt"` or `"fdt"`), `count`, `entries`: `address` and `mask` (BDT) or `ttl` and `remaining` in seconds (FDT) |
| `reinitialize`, `comm-control` | `target`, `request` (`"reinitialize-device"` or `"device-communication-control"`), `state`, `duration_minutes` (when given), `status` (`"acknowledged"`), `rtt_ms`. The password is never part of any output |
| `decode` | `length`, `layers`: `layer` (`"bvlc"`, `"npdu"`, `"apdu"`, `"service"`), `summary`, `fields` (`name`, `value`: a string, number or boolean); `error` when decoding stopped before the end. The fields of a layer are for reading: their names and values follow the frame, and are not part of the contract |
| `types`, `properties` | `what` (`"object-types"` or `"properties"`), `count`, `names`: `id`, `name`, `alias` |
| `version` | `version`, `go_version`, `os`, `arch` and, in a release build, `tag`, `commit`, `build_date` |

## Streams and stderr

- A streaming command (`monitor`, `events`) writes **compact** JSON, one object per line; every other command writes **one** indented JSON document.
- Progress, summaries and the `--debug` diagnostics go to **stderr**, as text, in every format. stdout holds nothing but the result.
- `file get --output -` is the exception that proves the rule: the file content takes stdout, and the result goes to stderr.
