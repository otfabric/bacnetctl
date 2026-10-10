// SPDX-License-Identifier: MIT

package bac

import (
	"strconv"
	"strings"

	bacnet "github.com/otfabric/go-bacnet"
)

// unitNames are the engineering units of ANSI/ASHRAE 135 (BACnetEngineeringUnits) up to
// 189; a later unit is written as its number.
var unitNames = [...]string{
	"square-meters", "square-feet", "milliamperes", "amperes", "ohms", "volts", "kilovolts", "megavolts",
	"volt-amperes", "kilovolt-amperes", "megavolt-amperes", "volt-amperes-reactive", "kilovolt-amperes-reactive",
	"megavolt-amperes-reactive", "degrees-phase", "power-factor", "joules", "kilojoules", "watt-hours",
	"kilowatt-hours", "btus", "therms", "ton-hours", "joules-per-kilogram-dry-air", "btus-per-pound-dry-air",
	"cycles-per-hour", "cycles-per-minute", "hertz", "grams-of-water-per-kilogram-dry-air",
	"percent-relative-humidity", "millimeters", "meters", "inches", "feet", "watts-per-square-foot",
	"watts-per-square-meter", "lumens", "luxes", "foot-candles", "kilograms", "pounds-mass", "tons",
	"kilograms-per-second", "kilograms-per-minute", "kilograms-per-hour", "pounds-mass-per-minute",
	"pounds-mass-per-hour", "watts", "kilowatts", "megawatts", "btus-per-hour", "horsepower",
	"tons-refrigeration", "pascals", "kilopascals", "bars", "pounds-force-per-square-inch",
	"centimeters-of-water", "inches-of-water", "millimeters-of-mercury", "centimeters-of-mercury",
	"inches-of-mercury", "degrees-celsius", "degrees-kelvin", "degrees-fahrenheit", "degree-days-celsius",
	"degree-days-fahrenheit", "years", "months", "weeks", "days", "hours", "minutes", "seconds",
	"meters-per-second", "kilometers-per-hour", "feet-per-second", "feet-per-minute", "miles-per-hour",
	"cubic-feet", "cubic-meters", "imperial-gallons", "liters", "us-gallons", "cubic-feet-per-minute",
	"cubic-meters-per-second", "imperial-gallons-per-minute", "liters-per-second", "liters-per-minute",
	"us-gallons-per-minute", "degrees-angular", "degrees-celsius-per-hour", "degrees-celsius-per-minute",
	"degrees-fahrenheit-per-hour", "degrees-fahrenheit-per-minute", "no-units", "parts-per-million",
	"parts-per-billion", "percent", "percent-per-second", "per-minute", "per-second",
	"psi-per-degree-fahrenheit", "radians", "revolutions-per-minute", "currency1", "currency2", "currency3",
	"currency4", "currency5", "currency6", "currency7", "currency8", "currency9", "currency10",
	"square-inches", "square-centimeters", "btus-per-pound", "centimeters", "pounds-mass-per-second",
	"delta-degrees-fahrenheit", "delta-degrees-kelvin", "kilohms", "megohms", "millivolts",
	"kilojoules-per-kilogram", "megajoules", "joules-per-degree-kelvin", "joules-per-kilogram-degree-kelvin",
	"kilohertz", "megahertz", "per-hour", "milliwatts", "hectopascals", "millibars", "cubic-meters-per-hour",
	"liters-per-hour", "kilowatt-hours-per-square-meter", "kilowatt-hours-per-square-foot",
	"megajoules-per-square-meter", "megajoules-per-square-foot", "watts-per-square-meter-degree-kelvin",
	"cubic-feet-per-second", "percent-obscuration-per-foot", "percent-obscuration-per-meter", "milliohms",
	"megawatt-hours", "kilo-btus", "mega-btus", "kilojoules-per-kilogram-dry-air",
	"megajoules-per-kilogram-dry-air", "kilojoules-per-degree-kelvin", "megajoules-per-degree-kelvin",
	"newton", "grams-per-second", "grams-per-minute", "tons-per-hour", "kilo-btus-per-hour",
	"hundredths-seconds", "milliseconds", "newton-meters", "millimeters-per-second", "millimeters-per-minute",
	"meters-per-minute", "meters-per-hour", "cubic-meters-per-minute", "meters-per-second-per-second",
	"amperes-per-meter", "amperes-per-square-meter", "ampere-square-meters", "farads", "henrys", "ohm-meters",
	"siemens", "siemens-per-meter", "teslas", "volts-per-degree-kelvin", "volts-per-meter", "webers",
	"candelas", "candelas-per-square-meter", "degrees-kelvin-per-hour", "degrees-kelvin-per-minute",
	"joule-seconds", "radians-per-second", "square-meters-per-newton", "kilograms-per-cubic-meter",
	"newton-seconds", "newtons-per-meter", "watts-per-meter-per-degree-kelvin",
}

// unitSymbols are the symbols shown next to a value. A unit without a symbol here is
// shown by its name.
var unitSymbols = map[uint32]string{
	0: "m²", 1: "ft²", 2: "mA", 3: "A", 4: "Ω", 5: "V", 6: "kV", 7: "MV", 8: "VA", 9: "kVA", 10: "MVA",
	11: "var", 12: "kvar", 13: "Mvar", 16: "J", 17: "kJ", 18: "Wh", 19: "kWh", 20: "BTU", 27: "Hz",
	29: "%RH", 30: "mm", 31: "m", 32: "in", 33: "ft", 35: "W/m²", 36: "lm", 37: "lx", 39: "kg", 40: "lb",
	41: "t", 42: "kg/s", 43: "kg/min", 44: "kg/h", 47: "W", 48: "kW", 49: "MW", 50: "BTU/h", 51: "hp",
	53: "Pa", 54: "kPa", 55: "bar", 56: "psi", 62: "°C", 63: "K", 64: "°F", 67: "a", 68: "mo", 69: "wk",
	70: "d", 71: "h", 72: "min", 73: "s", 74: "m/s", 75: "km/h", 76: "ft/s", 77: "ft/min", 78: "mph",
	79: "ft³", 80: "m³", 82: "L", 84: "cfm", 85: "m³/s", 87: "L/s", 88: "L/min", 89: "gpm", 90: "°",
	91: "°C/h", 92: "°C/min", 93: "°F/h", 94: "°F/min", 95: "", 96: "ppm", 97: "ppb", 98: "%", 99: "%/s",
	100: "/min", 101: "/s", 103: "rad", 104: "rpm", 118: "cm", 120: "Δ°F", 121: "ΔK", 122: "kΩ", 123: "MΩ",
	124: "mV", 125: "kJ/kg", 126: "MJ", 129: "kHz", 130: "MHz", 131: "/h", 132: "mW", 133: "hPa",
	134: "mbar", 135: "m³/h", 136: "L/h", 145: "mΩ", 146: "MWh", 153: "N", 154: "g/s", 155: "g/min",
	156: "t/h", 159: "ms", 160: "N·m", 161: "mm/s", 163: "m/min", 164: "m/h", 165: "m³/min", 166: "m/s²",
	170: "F", 171: "H", 173: "S", 175: "T", 178: "Wb", 179: "cd", 186: "kg/m³",
}

// UnitName returns the name of an engineering unit.
func UnitName(u uint32) string {
	if int(u) < len(unitNames) {
		return unitNames[u]
	}
	return strconv.FormatUint(uint64(u), 10)
}

// UnitSymbol returns what is written after a value with this unit: the symbol, the name
// for a unit without one, and nothing for no-units.
func UnitSymbol(u uint32) string {
	if s, ok := unitSymbols[u]; ok {
		return s
	}
	return UnitName(u)
}

var (
	binaryPVNames     = []string{"inactive", "active"}
	eventStateNames   = []string{"normal", "fault", "offnormal", "high-limit", "low-limit", "life-safety-alarm"}
	segmentationNames = []string{"segmented-both", "segmented-transmit", "segmented-receive", "no-segmentation"}
	systemStatusNames = []string{"operational", "operational-read-only", "download-required",
		"download-in-progress", "non-operational", "backup-in-progress"}
	reliabilityNames = []string{"no-fault-detected", "no-sensor", "over-range", "under-range", "open-loop",
		"shorted-loop", "no-output", "unreliable-other", "process-error", "multi-state-fault",
		"configuration-error", "", "communication-failure", "member-fault", "monitored-object-fault", "tripped"}
	polarityNames   = []string{"normal", "reverse"}
	notifyTypeNames = []string{"alarm", "event", "ack-notification"}
	fileAccessNames = []string{"record-access", "stream-access"}
	eventTypeNames  = []string{"change-of-bitstring", "change-of-state", "change-of-value", "command-failure",
		"floating-limit", "out-of-range", "complex-event-type", "", "change-of-life-safety", "extended",
		"buffer-ready", "unsigned-range", "", "access-event", "double-out-of-range", "signed-out-of-range",
		"unsigned-out-of-range", "change-of-characterstring", "change-of-status-flags",
		"change-of-reliability", "none", "change-of-discrete-value", "change-of-timer"}
	restartReasonNames = []string{"unknown", "coldstart", "warmstart", "detected-power-lost",
		"detected-powered-off", "hardware-watchdog", "software-watchdog", "suspended", "activate-changes"}
	loggingTypeNames = []string{"polled", "cov", "triggered"}

	statusFlagNames      = []string{"in-alarm", "fault", "overridden", "out-of-service"}
	eventTransitionNames = []string{"to-offnormal", "to-fault", "to-normal"}
	limitEnableNames     = []string{"low-limit-enable", "high-limit-enable"}
	logStatusNames       = []string{"log-disabled", "buffer-purged", "log-interrupted"}
	// servicesSupportedNames are the bits of BACnetServicesSupported.
	servicesSupportedNames = []string{"acknowledge-alarm", "confirmed-cov-notification",
		"confirmed-event-notification", "get-alarm-summary", "get-enrollment-summary", "subscribe-cov",
		"atomic-read-file", "atomic-write-file", "add-list-element", "remove-list-element", "create-object",
		"delete-object", "read-property", "read-property-conditional", "read-property-multiple",
		"write-property", "write-property-multiple", "device-communication-control",
		"confirmed-private-transfer", "confirmed-text-message", "reinitialize-device", "vt-open", "vt-close",
		"vt-data", "authenticate", "request-key", "i-am", "i-have", "unconfirmed-cov-notification",
		"unconfirmed-event-notification", "unconfirmed-private-transfer", "unconfirmed-text-message",
		"time-synchronization", "who-has", "who-is", "read-range", "utc-time-synchronization",
		"life-safety-operation", "subscribe-cov-property", "get-event-information", "write-group",
		"subscribe-cov-property-multiple", "confirmed-cov-notification-multiple",
		"unconfirmed-cov-notification-multiple", "confirmed-audit-notification", "audit-log-query",
		"unconfirmed-audit-notification", "who-am-i", "you-are"}
)

// enumNames returns the names of the enumeration a property holds, or nil.
func enumNames(objType bacnet.ObjectType, prop bacnet.PropertyIdentifier) []string {
	switch prop {
	case bacnet.PropertyPresentValue, bacnet.PropertyRelinquishDefault, bacnet.PropertyPriorityArray, 6, 40:
		// Present value, relinquish default, priority array, alarm value, feedback value.
		switch objType {
		case bacnet.ObjectTypeBinaryInput, bacnet.ObjectTypeBinaryOutput, bacnet.ObjectTypeBinaryValue:
			return binaryPVNames
		default:
			return nil
		}
	case bacnet.PropertyEventState:
		return eventStateNames
	case bacnet.PropertySegmentation:
		return segmentationNames
	case bacnet.PropertySystemStatus:
		return systemStatusNames
	case bacnet.PropertyReliability:
		return reliabilityNames
	case 84:
		return polarityNames
	case 72:
		return notifyTypeNames
	case 41:
		return fileAccessNames
	case 37:
		return eventTypeNames
	case 196:
		return restartReasonNames
	case 197:
		return loggingTypeNames
	default:
		return nil
	}
}

// EnumName names an enumerated value of a property, or returns "".
func EnumName(objType bacnet.ObjectType, prop bacnet.PropertyIdentifier, v uint32) string {
	switch prop {
	case bacnet.PropertyUnits, 82, 20, 27, 50, 94:
		return UnitName(v)
	case bacnet.PropertyObjectType:
		return ObjectTypeName(bacnet.ObjectType(v))
	case bacnet.PropertyPropertyList:
		return PropertyName(bacnet.PropertyIdentifier(v))
	default:
		return nameAt(enumNames(objType, prop), v)
	}
}

func nameAt(names []string, v uint32) string {
	if int(v) < len(names) {
		return names[v]
	}
	return ""
}

// ParseEnum parses what a person typed for an enumerated property: a number, or one of the
// names of the enumeration of that property (active, inactive, degrees-celsius, …).
func ParseEnum(objType bacnet.ObjectType, prop bacnet.PropertyIdentifier, s string) (uint32, bool) {
	if n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 32); err == nil {
		return uint32(n), true
	}
	name := normalizeName(s)
	names := enumNames(objType, prop)
	if prop == bacnet.PropertyUnits {
		names = unitNames[:]
	}
	for i, n := range names {
		if n != "" && n == name {
			return uint32(i), true
		}
	}
	return 0, false
}

// bitNames returns the names of the bits of a bit string property, or nil.
func bitNames(prop bacnet.PropertyIdentifier) []string {
	switch prop {
	case bacnet.PropertyStatusFlags:
		return statusFlagNames
	case 0, 35:
		return eventTransitionNames
	case 52:
		return limitEnableNames
	case propServicesSupported:
		return servicesSupportedNames
	case propObjectTypesSupported:
		names := make([]string, 65)
		for t, n := range objectTypeNames {
			names[t] = n
		}
		return names
	default:
		return nil
	}
}

// BitList returns the bits of a bit string as booleans, most significant first.
func BitList(b bacnet.BitString) []bool {
	n := len(b.Bytes)*8 - int(b.UnusedBits)
	if n < 0 {
		n = 0
	}
	out := make([]bool, n)
	for i := range out {
		out[i] = b.Bytes[i/8]&(0x80>>(i%8)) != 0
	}
	return out
}

// SetBitNames returns the names of the bits that are set, with the bit number for a bit
// that has no name.
func SetBitNames(b bacnet.BitString, names []string) []string {
	out := []string{}
	for i, set := range BitList(b) {
		if !set {
			continue
		}
		if n := nameAt(names, uint32(i)); n != "" {
			out = append(out, n)
		} else {
			out = append(out, "bit-"+strconv.Itoa(i))
		}
	}
	return out
}
