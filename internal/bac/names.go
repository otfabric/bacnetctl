// SPDX-License-Identifier: MIT

// Package bac is what the commands of bacnetctl do: it opens the BACnet/IP client, addresses
// devices, parses objects, properties and values, runs the services and turns the results
// into the output models of package types.
package bac

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"

	bacnet "github.com/otfabric/go-bacnet"
)

// objectTypeNames are the standard object types of ANSI/ASHRAE 135 (BACnetObjectType).
var objectTypeNames = map[bacnet.ObjectType]string{
	0: "analog-input", 1: "analog-output", 2: "analog-value", 3: "binary-input", 4: "binary-output",
	5: "binary-value", 6: "calendar", 7: "command", 8: "device", 9: "event-enrollment",
	10: "file", 11: "group", 12: "loop", 13: "multi-state-input", 14: "multi-state-output",
	15: "notification-class", 16: "program", 17: "schedule", 18: "averaging", 19: "multi-state-value",
	20: "trend-log", 21: "life-safety-point", 22: "life-safety-zone", 23: "accumulator", 24: "pulse-converter",
	25: "event-log", 26: "global-group", 27: "trend-log-multiple", 28: "load-control", 29: "structured-view",
	30: "access-door", 31: "timer", 32: "access-credential", 33: "access-point", 34: "access-rights",
	35: "access-user", 36: "access-zone", 37: "credential-data-input", 39: "bitstring-value",
	40: "characterstring-value", 41: "date-pattern-value", 42: "date-value", 43: "datetime-pattern-value",
	44: "datetime-value", 45: "integer-value", 46: "large-analog-value", 47: "octetstring-value",
	48: "positive-integer-value", 49: "time-pattern-value", 50: "time-value", 51: "notification-forwarder",
	52: "alert-enrollment", 53: "channel", 54: "lighting-output", 55: "binary-lighting-output",
	56: "network-port", 57: "elevator-group", 58: "escalator", 59: "lift", 60: "staging",
	61: "audit-log", 62: "audit-reporter", 63: "color", 64: "color-temperature",
}

// objectTypeAliases are the short forms accepted wherever an object type is typed.
var objectTypeAliases = map[string]bacnet.ObjectType{
	"ai": 0, "ao": 1, "av": 2, "bi": 3, "bo": 4, "bv": 5, "cal": 6, "dev": 8, "ee": 9,
	"msi": 13, "mso": 14, "msv": 19, "nc": 15, "sch": 17, "tl": 20, "tlm": 27, "np": 56,
}

// propertyNames are the standard property identifiers of ANSI/ASHRAE 135
// (BACnetPropertyIdentifier). The table is not complete: an identifier without a name is
// written as its number.
var propertyNames = map[bacnet.PropertyIdentifier]string{
	0: "acked-transitions", 1: "ack-required", 2: "action", 3: "action-text", 4: "active-text",
	5: "active-vt-sessions", 6: "alarm-value", 7: "alarm-values", 8: "all", 9: "all-writes-successful",
	10: "apdu-segment-timeout", 11: "apdu-timeout", 12: "application-software-version", 13: "archive", 14: "bias",
	15: "change-of-state-count", 16: "change-of-state-time", 17: "notification-class",
	19: "controlled-variable-reference", 20: "controlled-variable-units", 21: "controlled-variable-value",
	22: "cov-increment", 23: "date-list", 24: "daylight-savings-status", 25: "deadband",
	26: "derivative-constant", 27: "derivative-constant-units", 28: "description", 29: "description-of-halt",
	30: "device-address-binding", 31: "device-type", 32: "effective-period", 33: "elapsed-active-time",
	34: "error-limit", 35: "event-enable", 36: "event-state", 37: "event-type", 38: "exception-schedule",
	39: "fault-values", 40: "feedback-value", 41: "file-access-method", 42: "file-size", 43: "file-type",
	44: "firmware-revision", 45: "high-limit", 46: "inactive-text", 47: "in-process", 48: "instance-of",
	49: "integral-constant", 50: "integral-constant-units", 52: "limit-enable", 53: "list-of-group-members",
	54: "list-of-object-property-references", 56: "local-date", 57: "local-time", 58: "location", 59: "low-limit",
	60: "manipulated-variable-reference", 61: "maximum-output", 62: "max-apdu-length-accepted",
	63: "max-info-frames", 64: "max-master", 65: "max-pres-value", 66: "minimum-off-time", 67: "minimum-on-time",
	68: "minimum-output", 69: "min-pres-value", 70: "model-name", 71: "modification-date", 72: "notify-type",
	73: "number-of-apdu-retries", 74: "number-of-states", 75: "object-identifier", 76: "object-list",
	77: "object-name", 78: "object-property-reference", 79: "object-type", 80: "optional", 81: "out-of-service",
	82: "output-units", 83: "event-parameters", 84: "polarity", 85: "present-value", 86: "priority",
	87: "priority-array", 88: "priority-for-writing", 89: "process-identifier", 90: "program-change",
	91: "program-location", 92: "program-state", 93: "proportional-constant", 94: "proportional-constant-units",
	96: "protocol-object-types-supported", 97: "protocol-services-supported", 98: "protocol-version",
	99: "read-only", 100: "reason-for-halt", 102: "recipient-list", 103: "reliability", 104: "relinquish-default",
	105: "required", 106: "resolution", 107: "segmentation-supported", 108: "setpoint", 109: "setpoint-reference",
	110: "state-text", 111: "status-flags", 112: "system-status", 113: "time-delay",
	114: "time-of-active-time-reset", 115: "time-of-state-count-reset", 116: "time-synchronization-recipients",
	117: "units", 118: "update-interval", 119: "utc-offset", 120: "vendor-identifier", 121: "vendor-name",
	122: "vt-classes-supported", 123: "weekly-schedule", 124: "attempted-samples", 125: "average-value",
	126: "buffer-size", 127: "client-cov-increment", 128: "cov-resubscription-interval", 130: "event-time-stamps",
	131: "log-buffer", 132: "log-device-object-property", 133: "log-enable", 134: "log-interval",
	135: "maximum-value", 136: "minimum-value", 137: "notification-threshold", 139: "protocol-revision",
	140: "records-since-notification", 141: "record-count", 142: "start-time", 143: "stop-time",
	144: "stop-when-full", 145: "total-record-count", 146: "valid-samples", 147: "window-interval",
	148: "window-samples", 149: "maximum-value-timestamp", 150: "minimum-value-timestamp", 151: "variance-value",
	152: "active-cov-subscriptions", 153: "backup-failure-timeout", 154: "configuration-files",
	155: "database-revision", 156: "direct-reading", 157: "last-restore-time", 158: "maintenance-required",
	159: "member-of", 160: "mode", 161: "operation-expected", 162: "setting", 163: "silenced",
	164: "tracking-value", 165: "zone-members", 166: "life-safety-alarm-values", 167: "max-segments-accepted",
	168: "profile-name", 169: "auto-slave-discovery", 170: "manual-slave-address-binding",
	171: "slave-address-binding", 172: "slave-proxy-enable", 173: "last-notify-record", 174: "schedule-default",
	175: "accepted-modes", 176: "adjust-value", 177: "count", 178: "count-before-change", 179: "count-change-time",
	180: "cov-period", 181: "input-reference", 182: "limit-monitoring-interval", 183: "logging-object",
	184: "logging-record", 185: "prescale", 186: "pulse-rate", 187: "scale", 188: "scale-factor",
	189: "update-time", 190: "value-before-change", 191: "value-set", 192: "value-change-time",
	193: "align-intervals", 195: "interval-offset", 196: "last-restart-reason", 197: "logging-type",
	202: "restart-notification-recipients", 203: "time-of-device-restart", 204: "time-synchronization-interval",
	205: "trigger", 206: "utc-time-synchronization-recipients", 207: "node-subtype", 208: "node-type",
	209: "structured-object-list", 210: "subordinate-annotations", 211: "subordinate-list",
	212: "actual-shed-level", 213: "duty-window", 214: "expected-shed-level", 215: "full-duty-baseline",
	218: "requested-shed-level", 219: "shed-duration", 220: "shed-level-descriptions", 221: "shed-levels",
	222: "state-description", 226: "door-alarm-state", 227: "door-extended-pulse-time", 228: "door-members",
	229: "door-open-too-long-time", 230: "door-pulse-time", 231: "door-status", 232: "door-unlock-delay-time",
	233: "lock-status", 234: "masked-alarm-values", 235: "secured-status",
	351: "event-message-texts", 352: "event-message-texts-config", 353: "event-detection-enable",
	354: "event-algorithm-inhibit", 355: "event-algorithm-inhibit-ref", 356: "time-delay-normal",
	357: "reliability-evaluation-inhibit", 358: "fault-parameters", 359: "fault-type",
	360: "local-forwarding-only", 361: "process-identifier-filter", 362: "subscribed-recipients",
	363: "port-filter", 364: "authorization-exemptions", 365: "allow-group-delay-inhibit",
	366: "channel-number", 367: "control-groups", 368: "execution-delay", 369: "last-priority",
	370: "write-status", 371: "property-list", 372: "serial-number", 373: "blink-warn-enable",
	374: "default-fade-time", 375: "default-ramp-rate", 376: "default-step-increment", 377: "egress-time",
	378: "in-progress", 379: "instantaneous-power", 380: "lighting-command",
	381: "lighting-command-default-priority", 382: "max-actual-value", 383: "min-actual-value", 384: "power",
	385: "transition", 386: "egress-active", 387: "interface-value", 388: "fault-high-limit",
	389: "fault-low-limit", 390: "low-diff-limit", 391: "strike-count", 392: "time-of-strike-count-reset",
	393: "default-timeout", 394: "initial-timeout", 395: "last-state-change", 396: "state-change-values",
	397: "timer-running", 398: "timer-state", 399: "apdu-length", 400: "ip-address", 401: "ip-default-gateway",
	402: "ip-dhcp-enable", 403: "ip-dhcp-lease-time", 404: "ip-dhcp-lease-time-remaining", 405: "ip-dhcp-server",
	406: "ip-dns-server", 407: "bacnet-ip-global-address", 408: "bacnet-ip-mode",
	409: "bacnet-ip-multicast-address", 410: "bacnet-ip-nat-traversal", 411: "ip-subnet-mask",
	412: "bacnet-ip-udp-port", 413: "bbmd-accept-fd-registrations", 414: "bbmd-broadcast-distribution-table",
	415: "bbmd-foreign-device-table", 416: "changes-pending", 417: "command", 418: "fd-bbmd-address",
	419: "fd-subscription-lifetime", 420: "link-speed", 421: "link-speeds", 422: "link-speed-autonegotiate",
	423: "mac-address", 424: "network-interface-name", 425: "network-number", 426: "network-number-quality",
	427: "network-type", 428: "routing-table", 429: "virtual-mac-address-table", 430: "command-time-array",
	431: "current-command-priority", 432: "last-command-time", 433: "value-source", 434: "value-source-array",
}

// propertyAliases are the short forms accepted wherever a property is typed.
var propertyAliases = map[string]bacnet.PropertyIdentifier{
	"pv": bacnet.PropertyPresentValue, "name": bacnet.PropertyObjectName, "desc": bacnet.PropertyDescription,
	"flags": bacnet.PropertyStatusFlags, "oos": 81,
}

// The special property identifiers of ReadPropertyMultiple.
const (
	PropertyAll      = bacnet.PropertyAll
	PropertyOptional = bacnet.PropertyOptional
	PropertyRequired = bacnet.PropertyRequired
)

// Properties bacnetctl names in its own code and the library has no constant for.
const (
	propOutOfService         bacnet.PropertyIdentifier = 81
	propFirmwareRevision     bacnet.PropertyIdentifier = 44
	propApplicationSoftware  bacnet.PropertyIdentifier = 12
	propLocation             bacnet.PropertyIdentifier = 58
	propObjectTypesSupported bacnet.PropertyIdentifier = 96
	propServicesSupported    bacnet.PropertyIdentifier = 97
	propMaxSegmentsAccepted  bacnet.PropertyIdentifier = 167
	propAPDUTimeout          bacnet.PropertyIdentifier = 11
	propAPDURetries          bacnet.PropertyIdentifier = 73
	propDatabaseRevision     bacnet.PropertyIdentifier = 155
	propSerialNumber         bacnet.PropertyIdentifier = 372
	propLocalDate            bacnet.PropertyIdentifier = 56
	propLocalTime            bacnet.PropertyIdentifier = 57
	propUTCOffset            bacnet.PropertyIdentifier = 119
	propRecordCount          bacnet.PropertyIdentifier = 141
)

var (
	objectTypeByName = reverse(objectTypeNames)
	propertyByName   = reverse(propertyNames)
)

func reverse[K comparable](m map[K]string) map[string]K {
	out := make(map[string]K, len(m))
	for k, v := range m {
		out[v] = k
	}
	return out
}

// ObjectTypeName returns the name of an object type, or its number for a type that has
// none (a proprietary type, 128 and above, or one newer than this table).
func ObjectTypeName(t bacnet.ObjectType) string {
	if s, ok := objectTypeNames[t]; ok {
		return s
	}
	return strconv.Itoa(int(t))
}

// PropertyName returns the name of a property identifier, or its number for one that has
// none (a proprietary property, 512 and above, or one this table lacks).
func PropertyName(p bacnet.PropertyIdentifier) string {
	if s, ok := propertyNames[p]; ok {
		return s
	}
	return strconv.FormatUint(uint64(p), 10)
}

// ObjectName returns an object identifier as type:instance, with the name of the type.
func ObjectName(id bacnet.ObjectIdentifier) string {
	return ObjectTypeName(id.Type) + ":" + strconv.FormatUint(uint64(id.Instance), 10)
}

// normalizeName turns what a person types into the form of the name tables: lower case
// with hyphens. presentValue, Present_Value and "present value" all become present-value.
func normalizeName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '_' || r == ' ':
			b.WriteByte('-')
		case unicode.IsUpper(r):
			// A capital after a lower-case letter or digit starts a new word.
			if i > 0 && (unicode.IsLower(runes[i-1]) || unicode.IsDigit(runes[i-1])) {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(r))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ParseObjectType parses an object type: a name (analog-input), a short form (ai) or a
// number (0..1023).
func ParseObjectType(s string) (bacnet.ObjectType, error) {
	name := normalizeName(s)
	if name == "" {
		return 0, fmt.Errorf("empty object type")
	}
	if n, err := strconv.ParseUint(name, 10, 16); err == nil {
		if n > 1023 {
			return 0, fmt.Errorf("object type %d is out of range (0-1023)", n)
		}
		return bacnet.ObjectType(n), nil
	}
	if t, ok := objectTypeByName[name]; ok {
		return t, nil
	}
	if t, ok := objectTypeAliases[name]; ok {
		return t, nil
	}
	return 0, fmt.Errorf("unknown object type %q (see \"bacnetctl types\"; a number works too)", s)
}

// ParseObject parses an object identifier: type:instance, as in analog-input:1, ai:1 or
// 0:1. "device" alone is the Device object of whichever device is addressed.
func ParseObject(s string) (bacnet.ObjectIdentifier, error) {
	s = strings.TrimSpace(s)
	typ, inst, ok := strings.Cut(s, ":")
	if !ok {
		if t, err := ParseObjectType(s); err == nil && t == bacnet.ObjectTypeDevice {
			return bacnet.AnyDevice(), nil
		}
		return bacnet.ObjectIdentifier{}, fmt.Errorf("object %q: use type:instance, for example analog-input:1 or ai:1", s)
	}
	t, err := ParseObjectType(typ)
	if err != nil {
		return bacnet.ObjectIdentifier{}, fmt.Errorf("object %q: %w", s, err)
	}
	n, err := strconv.ParseUint(strings.TrimSpace(inst), 10, 32)
	if err != nil || n > bacnet.MaxObjectInstance {
		return bacnet.ObjectIdentifier{}, fmt.Errorf("object %q: the instance must be a number from 0 to %d", s, bacnet.MaxObjectInstance)
	}
	return bacnet.ObjectIdentifier{Type: t, Instance: uint32(n)}, nil
}

// ParseObjects parses a list of object identifiers.
func ParseObjects(args []string) ([]bacnet.ObjectIdentifier, error) {
	out := make([]bacnet.ObjectIdentifier, 0, len(args))
	for _, a := range args {
		id, err := ParseObject(a)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// ParseObjectTypes parses a comma-separated list of object types; empty gives nil.
func ParseObjectTypes(s string) ([]bacnet.ObjectType, error) {
	var out []bacnet.ObjectType
	for _, part := range strings.Split(s, ",") {
		if strings.TrimSpace(part) == "" {
			continue
		}
		t, err := ParseObjectType(part)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// ParseProperty parses a property identifier: a name (present-value), a short form (pv)
// or a number.
func ParseProperty(s string) (bacnet.PropertyIdentifier, error) {
	name := normalizeName(s)
	if name == "" {
		return 0, fmt.Errorf("empty property")
	}
	if n, err := strconv.ParseUint(name, 10, 32); err == nil {
		if n > 4194303 {
			return 0, fmt.Errorf("property %d is out of range (0-4194303)", n)
		}
		return bacnet.PropertyIdentifier(n), nil
	}
	if p, ok := propertyByName[name]; ok {
		return p, nil
	}
	if p, ok := propertyAliases[name]; ok {
		return p, nil
	}
	return 0, fmt.Errorf("unknown property %q (see \"bacnetctl properties\"; a number works too)", s)
}

// ParseProperties parses a comma-separated list of properties. Every property may carry
// an array index in brackets, as in priority-array[8].
func ParseProperties(s string) ([]bacnet.PropertyReference, error) {
	var out []bacnet.PropertyReference
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		ref, err := parsePropertyReference(part)
		if err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no property given")
	}
	return out, nil
}

func parsePropertyReference(s string) (bacnet.PropertyReference, error) {
	var ref bacnet.PropertyReference
	name := s
	if i := strings.IndexByte(s, '['); i >= 0 {
		if !strings.HasSuffix(s, "]") {
			return ref, fmt.Errorf("property %q: the array index is written as name[index]", s)
		}
		n, err := strconv.ParseUint(s[i+1:len(s)-1], 10, 32)
		if err != nil || uint32(n) == bacnet.ArrayAll {
			return ref, fmt.Errorf("property %q: the array index must be a number", s)
		}
		idx := uint32(n)
		ref.ArrayIndex = &idx
		name = s[:i]
	}
	id, err := ParseProperty(name)
	if err != nil {
		return ref, err
	}
	ref.Identifier = id
	return ref, nil
}

// NamedNumber is one row of a name table.
type NamedNumber struct {
	Number uint32
	Name   string
	Alias  string
}

// ObjectTypes lists the object types bacnetctl knows by name, in numeric order.
func ObjectTypes() []NamedNumber {
	aliases := map[bacnet.ObjectType][]string{}
	for a, t := range objectTypeAliases {
		aliases[t] = append(aliases[t], a)
	}
	out := make([]NamedNumber, 0, len(objectTypeNames))
	for t, name := range objectTypeNames {
		sort.Strings(aliases[t])
		out = append(out, NamedNumber{Number: uint32(t), Name: name, Alias: strings.Join(aliases[t], ",")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// Properties lists the property identifiers bacnetctl knows by name, in numeric order.
func Properties() []NamedNumber {
	aliases := map[bacnet.PropertyIdentifier][]string{}
	for a, p := range propertyAliases {
		aliases[p] = append(aliases[p], a)
	}
	out := make([]NamedNumber, 0, len(propertyNames))
	for p, name := range propertyNames {
		sort.Strings(aliases[p])
		out = append(out, NamedNumber{Number: uint32(p), Name: name, Alias: strings.Join(aliases[p], ",")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// ObjectTypeNames returns the names and short forms of the object types, for completion.
func ObjectTypeNames() []string {
	out := make([]string, 0, len(objectTypeNames)+len(objectTypeAliases))
	for _, n := range objectTypeNames {
		out = append(out, n)
	}
	for a := range objectTypeAliases {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// PropertyNames returns the names of the properties, for completion.
func PropertyNames() []string {
	out := make([]string, 0, len(propertyNames))
	for _, n := range propertyNames {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}
