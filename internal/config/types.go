// SPDX-License-Identifier: MIT

// Package config holds the configuration of every command: one struct per command, whose
// fields are bound to flags and environment variables by their tags.
package config

// SocketConfig is how bacnetctl opens its own BACnet/IP socket. Every command that talks
// to a network has it.
type SocketConfig struct {
	LocalPort uint16 `env:"BACNETCTL_LOCAL_PORT" desc:"Local UDP port to bind (0 = a free port chosen by the system)" flag:"local-port"`
	Interface string `env:"BACNETCTL_INTERFACE" desc:"Network interface whose subnet broadcast address is used for broadcasts (default: the subnet broadcast address of every interface, and 255.255.255.255)" flag:"interface"`
	BBMD      string `env:"BACNETCTL_BBMD" desc:"Register as a foreign device with this BBMD, host[:port], and send broadcasts through it" flag:"bbmd"`
	BBMDTTL   uint16 `env:"BACNETCTL_BBMD_TTL" desc:"Time-to-live of the foreign device registration in seconds" flag:"bbmd-ttl"`
	Timeout   uint32 `env:"BACNETCTL_TIMEOUT" desc:"APDU timeout in milliseconds: how long to wait for the answer to one request (0 = 3000)" flag:"timeout"`
	Retries   uint8  `env:"BACNETCTL_RETRIES" desc:"Retransmissions of an unanswered read request (writes are never sent twice)" flag:"retries"`
	Debug     bool   // set from global --debug (root persistent flag)
}

// ConnConfig is a socket plus the device a command addresses.
type ConnConfig struct {
	SocketConfig
	Device  string `env:"BACNETCTL_DEVICE" desc:"Device address: host[:port] (port 47808 by default). With --network and --mac it is the BACnet router in front of the device" flag:"device"`
	Network uint16 `env:"BACNETCTL_NETWORK" desc:"BACnet network number of a device behind a router (0 = the device is at --device itself)" flag:"network"`
	MAC     string `env:"BACNETCTL_MAC" desc:"MAC address of a device behind a router, in hex (for example 0a for MS/TP station 10)" flag:"mac"`
}

// DiscoverConfig drives `discover`.
type DiscoverConfig struct {
	SocketConfig
	Low          string `env:"BACNETCTL_LOW" desc:"Lowest device instance to ask for (with --high)" flag:"low"`
	High         string `env:"BACNETCTL_HIGH" desc:"Highest device instance to ask for (with --low)" flag:"high"`
	Wait         uint32 `env:"BACNETCTL_WAIT" desc:"How long to collect answers, in milliseconds" flag:"wait"`
	Target       string `env:"BACNETCTL_TARGET" desc:"Send the Who-Is to this address, host[:port], instead of broadcasting it" flag:"target"`
	Network      string `env:"BACNETCTL_DISCOVER_NETWORK" desc:"Ask only this BACnet network (1-65534), or all networks behind routers (all, the default for a broadcast)" flag:"network"`
	Names        bool   `env:"BACNETCTL_NAMES" desc:"Also read object-name, vendor-name and model-name of every device found" flag:"names"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// WhoHasConfig drives `whohas`.
type WhoHasConfig struct {
	SocketConfig
	Name         string `env:"BACNETCTL_NAME" desc:"Object name to look for (exact match)" flag:"name"`
	Object       string `env:"BACNETCTL_OBJECT" desc:"Object identifier to look for, type:instance" flag:"object"`
	Low          string `env:"BACNETCTL_LOW" desc:"Lowest device instance to ask (with --high)" flag:"low"`
	High         string `env:"BACNETCTL_HIGH" desc:"Highest device instance to ask (with --low)" flag:"high"`
	Wait         uint32 `env:"BACNETCTL_WAIT" desc:"How long to collect answers, in milliseconds" flag:"wait"`
	Target       string `env:"BACNETCTL_TARGET" desc:"Send the Who-Has to this address, host[:port], instead of broadcasting it" flag:"target"`
	Network      string `env:"BACNETCTL_DISCOVER_NETWORK" desc:"Ask only this BACnet network (1-65534), or all networks behind routers (all)" flag:"network"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ReadConfig drives `read`.
type ReadConfig struct {
	ConnConfig
	Index        string `env:"BACNETCTL_INDEX" desc:"Array index to read of every property (0 = the number of elements)" flag:"index"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// WriteConfig drives `write`.
type WriteConfig struct {
	ConnConfig
	Priority     uint8  `env:"BACNETCTL_PRIORITY" desc:"Command priority 1-16 for a commandable property (0 = none: the device uses 16)" flag:"priority"`
	Index        string `env:"BACNETCTL_INDEX" desc:"Array index of the element to write" flag:"index"`
	Type         string `env:"BACNETCTL_TYPE" desc:"Type of VALUE: auto, null, bool, unsigned, signed, real, double, string, octets, bits, enum, date, time or object" flag:"type"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ReleaseConfig drives `release`.
type ReleaseConfig struct {
	ConnConfig
	Priority     uint8  `env:"BACNETCTL_PRIORITY" desc:"Command priority 1-16 to relinquish" flag:"priority"`
	Property     string `env:"BACNETCTL_PROPERTY" desc:"Commandable property to relinquish" flag:"property"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// CreateConfig drives `create`.
type CreateConfig struct {
	ConnConfig
	Name         string `env:"BACNETCTL_NAME" desc:"Name of the new object (its object-name property)" flag:"name"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// DeleteConfig drives `delete`.
type DeleteConfig struct {
	ConnConfig
	// Sure is --yes-i-am-sure. It has no environment variable: the confirmation belongs on
	// the command line of the one invocation it is meant for.
	Sure         bool
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ObjectsConfig drives `objects`.
type ObjectsConfig struct {
	ConnConfig
	Values       bool   `env:"BACNETCTL_VALUES" desc:"Also read present-value, units, status-flags and description of every object" flag:"values"`
	Types        string `env:"BACNETCTL_TYPES" desc:"Only list these object types: comma-separated names, short forms or numbers (e.g. ai,av,binary-value)" flag:"type"`
	Limit        uint32 `env:"BACNETCTL_LIMIT" desc:"List at most this many objects (0 = all)" flag:"limit"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// DescribeConfig drives `describe`.
type DescribeConfig struct {
	ConnConfig
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// DeviceConfig drives `device`.
type DeviceConfig struct {
	ConnConfig
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// MonitorConfig drives `monitor`.
type MonitorConfig struct {
	ConnConfig
	Property     string `env:"BACNETCTL_PROPERTY" desc:"Subscribe to this property only (SubscribeCOVProperty); with --poll, the property to poll (default present-value)" flag:"property"`
	Increment    string `env:"BACNETCTL_INCREMENT" desc:"COV increment for --property: the change that triggers a notification (default: the device's own)" flag:"increment"`
	Confirmed    bool   `env:"BACNETCTL_CONFIRMED" desc:"Ask for confirmed notifications (bacnetctl acknowledges each one)" flag:"confirmed"`
	Lifetime     uint32 `env:"BACNETCTL_LIFETIME" desc:"Lifetime of the subscriptions in seconds; they are renewed while monitoring runs" flag:"lifetime"`
	Poll         uint32 `env:"BACNETCTL_POLL" desc:"Do not subscribe: read the property every this many milliseconds and print it when it changed (0 = subscribe)" flag:"poll"`
	Duration     uint32 `env:"BACNETCTL_DURATION" desc:"Stop after this many seconds (0 = until interrupted)" flag:"duration"`
	Count        uint32 `env:"BACNETCTL_COUNT" desc:"Stop after this many notifications (0 = until interrupted)" flag:"count"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// AlarmsConfig drives `alarms`.
type AlarmsConfig struct {
	ConnConfig
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// AckConfig drives `ack`.
type AckConfig struct {
	ConnConfig
	State        string `env:"BACNETCTL_STATE" desc:"Event state that is acknowledged: offnormal, fault, normal, high-limit, low-limit or life-safety-alarm (default: the state the object is in)" flag:"state"`
	Sequence     string `env:"BACNETCTL_SEQUENCE" desc:"Sequence number time stamp of the event that is acknowledged (as the event notification carried it)" flag:"sequence"`
	Time         string `env:"BACNETCTL_EVENT_TIME" desc:"Date and time stamp of the event that is acknowledged, RFC 3339 (as the event notification carried it)" flag:"event-time"`
	Source       string `env:"BACNETCTL_SOURCE" desc:"Who acknowledges (acknowledgment source)" flag:"source"`
	ProcessID    uint32 `env:"BACNETCTL_PROCESS_ID" desc:"Acknowledging process identifier" flag:"process-id"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// EventsConfig drives `events`.
type EventsConfig struct {
	SocketConfig
	Duration     uint32 `env:"BACNETCTL_DURATION" desc:"Stop after this many seconds (0 = until interrupted)" flag:"duration"`
	Count        uint32 `env:"BACNETCTL_COUNT" desc:"Stop after this many notifications (0 = until interrupted)" flag:"count"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// TrendConfig drives `trend`.
type TrendConfig struct {
	ConnConfig
	Start        uint32 `env:"BACNETCTL_START" desc:"Position of the first record to read (1 = the oldest in the buffer)" flag:"start"`
	Count        uint32 `env:"BACNETCTL_COUNT" desc:"Number of records to read" flag:"count"`
	All          bool   `env:"BACNETCTL_ALL" desc:"Read the whole buffer from --start, in pages of --count records" flag:"all"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// FileGetConfig drives `file get`.
type FileGetConfig struct {
	ConnConfig
	Output       string `env:"BACNETCTL_OUTPUT" desc:"File to write the content to (- = stdout)" flag:"output"`
	MaxSize      uint32 `env:"BACNETCTL_MAX_SIZE" desc:"Refuse to read more than this many octets" flag:"max-size"`
	Chunk        uint32 `env:"BACNETCTL_CHUNK" desc:"Octets per AtomicReadFile request" flag:"chunk"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// FilePutConfig drives `file put`.
type FilePutConfig struct {
	ConnConfig
	Input        string `env:"BACNETCTL_INPUT" desc:"File whose content is written" flag:"input"`
	Start        uint32 `env:"BACNETCTL_FILE_START" desc:"Position in the file object to write at" flag:"start"`
	Chunk        uint32 `env:"BACNETCTL_CHUNK" desc:"Octets per AtomicWriteFile request" flag:"chunk"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// TimeSyncConfig drives `time sync`.
type TimeSyncConfig struct {
	ConnConfig
	Time         string `env:"BACNETCTL_TIME" desc:"Time to set: now, or an RFC 3339 time such as 2026-01-31T12:00:00+01:00" flag:"time"`
	UTC          bool   `env:"BACNETCTL_UTC" desc:"Send UTCTimeSynchronization (the time in UTC) instead of TimeSynchronization (local time)" flag:"utc"`
	Broadcast    bool   `env:"BACNETCTL_BROADCAST" desc:"Broadcast to every device on the local network instead of sending to --device" flag:"broadcast"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// RouterListConfig drives `router list`.
type RouterListConfig struct {
	SocketConfig
	Target       string `env:"BACNETCTL_TARGET" desc:"Ask this router, host[:port], instead of broadcasting the question" flag:"target"`
	Network      uint16 `env:"BACNETCTL_ROUTER_NETWORK" desc:"Ask only for the router to this network (0 = all networks)" flag:"network"`
	Wait         uint32 `env:"BACNETCTL_WAIT" desc:"How long to collect answers, in milliseconds" flag:"wait"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// BBMDTableConfig drives `bbmd bdt` and `bbmd fdt`.
type BBMDTableConfig struct {
	SocketConfig
	Device       string `env:"BACNETCTL_DEVICE" desc:"Address of the BBMD: host[:port] (port 47808 by default)" flag:"device"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ManageConfig is what the commands that change how a device runs share. The password is
// deliberately not bound here: it must never show up as the default of a flag in --help.
type ManageConfig struct {
	ConnConfig
	// Sure is --yes-i-am-sure. It has no environment variable: the confirmation belongs on
	// the command line of the one invocation it is meant for.
	Sure         bool
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// CommControlConfig drives `comm-control`.
type CommControlConfig struct {
	ManageConfig
	Duration uint16 `env:"BACNETCTL_COMM_DURATION" desc:"Minutes the device stays silent before it enables itself again (0 = until it is enabled)" flag:"duration"`
}

// VersionConfig drives `version`.
type VersionConfig struct {
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// DecodeConfig drives `decode`.
type DecodeConfig struct {
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}

// ListConfig drives `types` and `properties`.
type ListConfig struct {
	Filter       string `env:"BACNETCTL_FILTER" desc:"Only list names that contain this text" flag:"filter"`
	OutputFormat string `env:"BACNETCTL_OUTPUT_FORMAT" desc:"Output format: text, json, or table" flag:"format"`
}
