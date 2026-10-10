// SPDX-License-Identifier: MIT

package types

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

// ObjectRef names an object: "analog-input:1" for people, with the numbers of the type and
// the instance for programs.
type ObjectRef struct {
	Object     string `json:"object"`
	ObjectType uint16 `json:"object_type"`
	Instance   uint32 `json:"instance"`
}

// PropertyRef names a property, with its number and the array index when one was used.
type PropertyRef struct {
	Property   string  `json:"property"`
	PropertyID uint32  `json:"property_id"`
	Index      *uint32 `json:"index,omitempty"`
}

// Label returns the property as text: name, or name[index].
func (p PropertyRef) Label() string {
	if p.Index != nil {
		return fmt.Sprintf("%s[%d]", p.Property, *p.Index)
	}
	return p.Property
}

// grid renders rows as aligned columns under a header.
func grid(headers []string, rows [][]string) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, strings.Join(headers, "\t"))
	for _, row := range rows {
		_, _ = fmt.Fprintln(tw, strings.Join(row, "\t"))
	}
	_ = tw.Flush()
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return strings.Join(lines, "\n")
}

// listText is a summary line followed by the rows of a result as aligned columns.
func listText(summary string, headers []string, rows [][]string) string {
	if len(rows) == 0 {
		return summary
	}
	return summary + "\n\n" + grid(headers, rows)
}

// fields renders name/value pairs as two aligned columns, skipping empty values.
func fields(pairs ...string) string {
	width := 0
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" && len(pairs[i]) > width {
			width = len(pairs[i])
		}
	}
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			continue
		}
		_, _ = fmt.Fprintf(&b, "%-*s  %s\n", width+1, pairs[i]+":", pairs[i+1])
	}
	return strings.TrimRight(b.String(), "\n")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// flagsText shows status flags in a column: a dash for an object or record without any,
// "ok" when none is set, and the flags that are set otherwise.
func flagsText(flags []string) string {
	switch {
	case flags == nil:
		return "-"
	case len(flags) == 0:
		return "ok"
	default:
		return strings.Join(flags, ",")
	}
}

func seconds(ms float64) string {
	return strconv.FormatFloat(ms/1000, 'f', 1, 64) + " s"
}

func optUint[T uint8 | uint16 | uint32 | uint64](v *T) string {
	if v == nil {
		return ""
	}
	return strconv.FormatUint(uint64(*v), 10)
}

// ---- discover ----

// Device is one device that answered a Who-Is.
type Device struct {
	Instance uint32 `json:"instance"`
	// Address is the BACnet/IP address the I-Am came from: the device itself, or the
	// router in front of it.
	Address string `json:"address"`
	// Network and MAC are the BACnet address of a device behind a router.
	Network        *uint16    `json:"network,omitempty"`
	MAC            string     `json:"mac,omitempty"`
	VendorID       uint16     `json:"vendor_id"`
	MaxAPDU        uint16     `json:"max_apdu"`
	Segmentation   string     `json:"segmentation"`
	SegmentationID uint8      `json:"segmentation_id"`
	Name           string     `json:"name,omitempty"`
	VendorName     string     `json:"vendor_name,omitempty"`
	ModelName      string     `json:"model_name,omitempty"`
	Error          *ErrorInfo `json:"error,omitempty"`
}

// DiscoverResult is the output of `discover`.
type DiscoverResult struct {
	// Mode is "broadcast" or "unicast".
	Mode       string   `json:"mode"`
	Target     string   `json:"target,omitempty"`
	Names      bool     `json:"-"`
	DurationMS float64  `json:"duration_ms"`
	Count      int      `json:"count"`
	Devices    []Device `json:"devices"`
}

// TableHeaders implements format.TableMarshaler.
func (r *DiscoverResult) TableHeaders() []string {
	h := []string{"INSTANCE", "ADDRESS", "NETWORK", "MAC", "VENDOR", "MAX APDU", "SEGMENTATION"}
	if r.Names {
		h = append(h, "NAME", "VENDOR NAME", "MODEL")
	}
	return h
}

// TableRows implements format.TableMarshaler.
func (r *DiscoverResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Devices))
	for _, d := range r.Devices {
		row := []string{
			strconv.FormatUint(uint64(d.Instance), 10), d.Address, dash(optUint(d.Network)), dash(d.MAC),
			strconv.Itoa(int(d.VendorID)), strconv.Itoa(int(d.MaxAPDU)), d.Segmentation,
		}
		if r.Names {
			name := d.Name
			if d.Error != nil {
				name = ErrorText(d.Error)
			}
			row = append(row, dash(name), dash(d.VendorName), dash(d.ModelName))
		}
		rows = append(rows, row)
	}
	return rows
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *DiscoverResult) MarshalTextOutput() (string, error) {
	how := "broadcast Who-Is"
	if r.Mode == "unicast" {
		how = "Who-Is to " + r.Target
	}
	summary := fmt.Sprintf("%s found in %s (%s)", plural(r.Count, "device", "devices"), seconds(r.DurationMS), how)
	return listText(summary, r.TableHeaders(), r.TableRows()), nil
}

// ---- whohas ----

// FoundObject is one I-Have answer.
type FoundObject struct {
	ObjectRef
	Name    string  `json:"name"`
	Device  uint32  `json:"device"`
	Address string  `json:"address"`
	Network *uint16 `json:"network,omitempty"`
	MAC     string  `json:"mac,omitempty"`
}

// WhoHasResult is the output of `whohas`.
type WhoHasResult struct {
	Mode       string        `json:"mode"`
	Target     string        `json:"target,omitempty"`
	Query      string        `json:"query"`
	DurationMS float64       `json:"duration_ms"`
	Count      int           `json:"count"`
	Objects    []FoundObject `json:"objects"`
}

// TableHeaders implements format.TableMarshaler.
func (r *WhoHasResult) TableHeaders() []string {
	return []string{"DEVICE", "ADDRESS", "NETWORK", "MAC", "OBJECT", "NAME"}
}

// TableRows implements format.TableMarshaler.
func (r *WhoHasResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Objects))
	for _, o := range r.Objects {
		rows = append(rows, []string{strconv.FormatUint(uint64(o.Device), 10), o.Address, dash(optUint(o.Network)), dash(o.MAC), o.Object, o.Name})
	}
	return rows
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *WhoHasResult) MarshalTextOutput() (string, error) {
	summary := fmt.Sprintf("%s for %s in %s", plural(r.Count, "answer", "answers"), r.Query, seconds(r.DurationMS))
	return listText(summary, r.TableHeaders(), r.TableRows()), nil
}

// ---- read / describe ----

// PropertyRow is one property of an object: its value, or why it could not be read.
type PropertyRow struct {
	ObjectRef
	PropertyRef
	Value *Value     `json:"value,omitempty"`
	Error *ErrorInfo `json:"error,omitempty"`
}

func (p PropertyRow) cell() string {
	if p.Error != nil {
		return ErrorText(p.Error)
	}
	return TextOf(p.Value)
}

// ReadResult is the output of `read`.
type ReadResult struct {
	Target string `json:"target"`
	// Service is "read-property" or "read-property-multiple".
	Service string        `json:"service"`
	Count   int           `json:"count"`
	Failed  int           `json:"failed"`
	Rows    []PropertyRow `json:"rows"`
}

// TableHeaders implements format.TableMarshaler.
func (r *ReadResult) TableHeaders() []string { return []string{"OBJECT", "PROPERTY", "VALUE"} }

// TableRows implements format.TableMarshaler.
func (r *ReadResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Rows))
	for _, p := range r.Rows {
		rows = append(rows, []string{p.Object, p.Label(), p.cell()})
	}
	return rows
}

// MarshalTextOutput implements format.TextMarshaler. One value alone is printed bare, so
// that it can be used as it is.
func (r *ReadResult) MarshalTextOutput() (string, error) {
	if len(r.Rows) == 1 && r.Rows[0].Error == nil {
		return TextOf(r.Rows[0].Value), nil
	}
	return grid(r.TableHeaders(), r.TableRows()), nil
}

// DescribeResult is the output of `describe`.
type DescribeResult struct {
	Target string `json:"target"`
	ObjectRef
	Name       string        `json:"name,omitempty"`
	Count      int           `json:"count"`
	Failed     int           `json:"failed"`
	Properties []PropertyRow `json:"properties"`
}

// TableHeaders implements format.TableMarshaler.
func (r *DescribeResult) TableHeaders() []string { return []string{"PROPERTY", "ID", "TYPE", "VALUE"} }

// TableRows implements format.TableMarshaler.
func (r *DescribeResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Properties))
	for _, p := range r.Properties {
		typ := "-"
		if p.Value != nil {
			typ = p.Value.Type
		}
		rows = append(rows, []string{p.Label(), strconv.FormatUint(uint64(p.PropertyID), 10), typ, p.cell()})
	}
	return rows
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *DescribeResult) MarshalTextOutput() (string, error) {
	pairs := make([]string, 0, 2*len(r.Properties))
	for _, p := range r.Properties {
		pairs = append(pairs, p.Label(), p.cell())
	}
	head := fmt.Sprintf("%s at %s (%s)", r.Object, r.Target, plural(r.Count, "property", "properties"))
	return head + "\n\n" + fields(pairs...), nil
}

// ---- write / release ----

// WriteResult is the output of `write` and `release`.
type WriteResult struct {
	Target string `json:"target"`
	ObjectRef
	PropertyRef
	Value    Value  `json:"value"`
	Priority *uint8 `json:"priority,omitempty"`
	// Status is "written" or "relinquished".
	Status string  `json:"status"`
	RTTMS  float64 `json:"rtt_ms"`
}

// TableHeaders implements format.TableMarshaler.
func (r *WriteResult) TableHeaders() []string {
	return []string{"OBJECT", "PROPERTY", "VALUE", "PRIORITY", "STATUS"}
}

// TableRows implements format.TableMarshaler.
func (r *WriteResult) TableRows() [][]string {
	return [][]string{{r.Object, r.Label(), r.Value.String(), dash(optUint(r.Priority)), r.Status}}
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *WriteResult) MarshalTextOutput() (string, error) {
	at := ""
	if r.Priority != nil {
		at = fmt.Sprintf(" at priority %d", *r.Priority)
	}
	if r.Status == "relinquished" {
		return fmt.Sprintf("%s %s: relinquished%s (acknowledged by the device)", r.Object, r.Label(), at), nil
	}
	return fmt.Sprintf("%s %s = %s%s (acknowledged by the device)", r.Object, r.Label(), r.Value.String(), at), nil
}

// WriteRow is one write of a `write` with several.
type WriteRow struct {
	ObjectRef
	PropertyRef
	Value    Value  `json:"value"`
	Priority *uint8 `json:"priority,omitempty"`
	// Status is "written", "relinquished", "failed" (the device refused this write) or
	// "not-written" (it follows the one the device refused).
	Status string     `json:"status"`
	Error  *ErrorInfo `json:"error,omitempty"`
}

// WriteMultipleResult is the output of `write` with more than one property.
type WriteMultipleResult struct {
	Target string `json:"target"`
	// Service is "write-property-multiple".
	Service string `json:"service"`
	Count   int    `json:"count"`
	// Failed is 1 when the device refused a write, and NotWritten the number of writes
	// after it, which the device did not attempt.
	Failed     int        `json:"failed"`
	NotWritten int        `json:"not_written"`
	Writes     []WriteRow `json:"writes"`
	RTTMS      float64    `json:"rtt_ms"`
}

// TableHeaders implements format.TableMarshaler.
func (r *WriteMultipleResult) TableHeaders() []string {
	return []string{"OBJECT", "PROPERTY", "VALUE", "PRIORITY", "STATUS"}
}

// TableRows implements format.TableMarshaler.
func (r *WriteMultipleResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Writes))
	for _, w := range r.Writes {
		status := w.Status
		if w.Error != nil {
			status += ": " + w.Error.Message
		}
		rows = append(rows, []string{w.Object, w.Label(), w.Value.String(), dash(optUint(w.Priority)), status})
	}
	return rows
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *WriteMultipleResult) MarshalTextOutput() (string, error) {
	out := grid(r.TableHeaders(), r.TableRows())
	if r.Failed == 0 {
		return out + fmt.Sprintf("\n%d properties written (acknowledged by the device)", r.Count), nil
	}
	summary := fmt.Sprintf("\n%d of %d properties written: the device refused one", r.Count-r.Failed-r.NotWritten, r.Count)
	if r.NotWritten > 0 {
		summary += fmt.Sprintf(", and did not attempt the %d after it", r.NotWritten)
	}
	return out + summary, nil
}

// ---- create / delete ----

// ObjectChangeResult is the output of `create` and `delete`.
type ObjectChangeResult struct {
	Target string `json:"target"`
	ObjectRef
	// Status is "created" or "deleted".
	Status string  `json:"status"`
	RTTMS  float64 `json:"rtt_ms"`
}

// TableHeaders implements format.TableMarshaler.
func (r *ObjectChangeResult) TableHeaders() []string { return []string{"OBJECT", "STATUS"} }

// TableRows implements format.TableMarshaler.
func (r *ObjectChangeResult) TableRows() [][]string { return [][]string{{r.Object, r.Status}} }

// MarshalTextOutput implements format.TextMarshaler.
func (r *ObjectChangeResult) MarshalTextOutput() (string, error) {
	return fmt.Sprintf("%s: %s (acknowledged by the device)", r.Object, r.Status), nil
}

// ---- objects ----

// ObjectRow is one object of a device.
type ObjectRow struct {
	ObjectRef
	Name         string     `json:"name,omitempty"`
	PresentValue *Value     `json:"present_value,omitempty"`
	Units        string     `json:"units,omitempty"`
	UnitsID      *uint32    `json:"units_id,omitempty"`
	StatusFlags  []string   `json:"status_flags,omitempty"`
	Description  string     `json:"description,omitempty"`
	Error        *ErrorInfo `json:"error,omitempty"`
}

// MarshalJSON implements json.Marshaler. The status flags of an object that has them are
// always there, also when none is set.
func (o ObjectRow) MarshalJSON() ([]byte, error) {
	type plain ObjectRow
	out := struct {
		plain
		StatusFlags *[]string `json:"status_flags,omitempty"`
	}{plain: plain(o)}
	if o.StatusFlags != nil {
		out.StatusFlags = &o.StatusFlags
	}
	return json.Marshal(out)
}

// ObjectsResult is the output of `objects`.
type ObjectsResult struct {
	Target string `json:"target"`
	Device string `json:"device"`
	// Total is the number of objects of the device, Count the number listed.
	Total   int         `json:"total"`
	Count   int         `json:"count"`
	Failed  int         `json:"failed"`
	Values  bool        `json:"-"`
	Objects []ObjectRow `json:"objects"`
}

// TableHeaders implements format.TableMarshaler.
func (r *ObjectsResult) TableHeaders() []string {
	if r.Values {
		return []string{"OBJECT", "NAME", "VALUE", "STATUS", "DESCRIPTION"}
	}
	return []string{"OBJECT", "NAME"}
}

// TableRows implements format.TableMarshaler.
func (r *ObjectsResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Objects))
	for _, o := range r.Objects {
		name := dash(o.Name)
		if o.Error != nil {
			name = ErrorText(o.Error)
		}
		row := []string{o.Object, name}
		if r.Values {
			row = append(row, TextOf(o.PresentValue), flagsText(o.StatusFlags), dash(o.Description))
		}
		rows = append(rows, row)
	}
	return rows
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *ObjectsResult) MarshalTextOutput() (string, error) {
	summary := fmt.Sprintf("%s at %s: %s", r.Device, r.Target, plural(r.Total, "object", "objects"))
	if r.Count != r.Total {
		summary += fmt.Sprintf(", %d listed", r.Count)
	}
	return listText(summary, r.TableHeaders(), r.TableRows()), nil
}

// ---- device ----

// DeviceResult is the output of `device`: what a device says about itself. A property the
// device does not have is omitted.
type DeviceResult struct {
	Target string `json:"target"`
	ObjectRef
	Name                 string   `json:"name,omitempty"`
	Description          string   `json:"description,omitempty"`
	Location             string   `json:"location,omitempty"`
	VendorName           string   `json:"vendor_name,omitempty"`
	VendorID             *uint32  `json:"vendor_id,omitempty"`
	ModelName            string   `json:"model_name,omitempty"`
	SerialNumber         string   `json:"serial_number,omitempty"`
	FirmwareRevision     string   `json:"firmware_revision,omitempty"`
	ApplicationVersion   string   `json:"application_software_version,omitempty"`
	SystemStatus         string   `json:"system_status,omitempty"`
	SystemStatusID       *uint32  `json:"system_status_id,omitempty"`
	ProtocolVersion      *uint32  `json:"protocol_version,omitempty"`
	ProtocolRevision     *uint32  `json:"protocol_revision,omitempty"`
	MaxAPDU              *uint32  `json:"max_apdu,omitempty"`
	Segmentation         string   `json:"segmentation,omitempty"`
	SegmentationID       *uint32  `json:"segmentation_id,omitempty"`
	MaxSegments          *uint32  `json:"max_segments,omitempty"`
	APDUTimeoutMS        *uint32  `json:"apdu_timeout_ms,omitempty"`
	APDURetries          *uint32  `json:"apdu_retries,omitempty"`
	DatabaseRevision     *uint32  `json:"database_revision,omitempty"`
	LocalDate            string   `json:"local_date,omitempty"`
	LocalTime            string   `json:"local_time,omitempty"`
	UTCOffsetMinutes     *int64   `json:"utc_offset_minutes,omitempty"`
	ObjectCount          *uint32  `json:"object_count,omitempty"`
	ServicesSupported    []string `json:"services_supported,omitempty"`
	ObjectTypesSupported []string `json:"object_types_supported,omitempty"`
}

func wrapList(items []string, width int) string {
	var lines []string
	line := ""
	for _, it := range items {
		if line != "" && len(line)+2+len(it) > width {
			lines = append(lines, line+",")
			line = ""
		}
		if line != "" {
			line += ", "
		}
		line += it
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (r *DeviceResult) pairs() []string {
	enum := func(name string, id *uint32) string {
		if id == nil {
			return ""
		}
		if name == "" {
			return optUint(id)
		}
		return fmt.Sprintf("%s (%d)", name, *id)
	}
	offset := ""
	if r.UTCOffsetMinutes != nil {
		offset = strconv.FormatInt(*r.UTCOffsetMinutes, 10) + " min"
	}
	timeout := ""
	if r.APDUTimeoutMS != nil {
		timeout = optUint(r.APDUTimeoutMS) + " ms"
	}
	return []string{
		"Device", r.Object,
		"Address", r.Target,
		"Name", r.Name,
		"Description", r.Description,
		"Location", r.Location,
		"Vendor", r.VendorName,
		"Vendor ID", optUint(r.VendorID),
		"Model", r.ModelName,
		"Serial number", r.SerialNumber,
		"Firmware", r.FirmwareRevision,
		"Application software", r.ApplicationVersion,
		"System status", enum(r.SystemStatus, r.SystemStatusID),
		"Protocol version", optUint(r.ProtocolVersion),
		"Protocol revision", optUint(r.ProtocolRevision),
		"Max APDU", optUint(r.MaxAPDU),
		"Segmentation", enum(r.Segmentation, r.SegmentationID),
		"Max segments", optUint(r.MaxSegments),
		"APDU timeout", timeout,
		"APDU retries", optUint(r.APDURetries),
		"Database revision", optUint(r.DatabaseRevision),
		"Local date", r.LocalDate,
		"Local time", r.LocalTime,
		"UTC offset", offset,
		"Objects", optUint(r.ObjectCount),
	}
}

// TableHeaders implements format.TableMarshaler.
func (r *DeviceResult) TableHeaders() []string { return []string{"PROPERTY", "VALUE"} }

// TableRows implements format.TableMarshaler.
func (r *DeviceResult) TableRows() [][]string {
	var rows [][]string
	p := append(r.pairs(), "Services", strings.Join(r.ServicesSupported, ", "), "Object types", strings.Join(r.ObjectTypesSupported, ", "))
	for i := 0; i+1 < len(p); i += 2 {
		if p[i+1] != "" {
			rows = append(rows, []string{p[i], p[i+1]})
		}
	}
	return rows
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *DeviceResult) MarshalTextOutput() (string, error) {
	out := fields(r.pairs()...)
	for _, l := range []struct {
		title string
		items []string
	}{{"Services supported", r.ServicesSupported}, {"Object types supported", r.ObjectTypesSupported}} {
		if len(l.items) > 0 {
			out += fmt.Sprintf("\n\n%s (%d):\n  %s", l.title, len(l.items), strings.ReplaceAll(wrapList(l.items, 86), "\n", "\n  "))
		}
	}
	return out, nil
}

// ---- monitor ----

// PropertyValue is one property with its value in a notification.
type PropertyValue struct {
	PropertyRef
	Value Value `json:"value"`
}

// COVRecord is one change-of-value notification, or one polled change.
type COVRecord struct {
	// Timestamp is when the notification arrived, RFC 3339 with milliseconds, UTC.
	Timestamp string `json:"timestamp"`
	// Source is "cov" for a notification and "poll" for a polled value.
	Source string `json:"source"`
	// Device is the device that sent the notification.
	Device string `json:"device,omitempty"`
	ObjectRef
	// TimeRemaining is the remaining lifetime of the subscription in seconds.
	TimeRemaining *uint32         `json:"time_remaining,omitempty"`
	Values        []PropertyValue `json:"values"`
	// Gap is set when notifications before this one were lost because they arrived
	// faster than they were printed.
	Gap bool `json:"gap,omitempty"`
}

// clock shows the time of day of a record's time stamp in the local time zone: what a
// person at the terminal compares with a wall clock. JSON keeps the full UTC time stamp.
func clock(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		return ts
	}
	return t.Local().Format("15:04:05.000")
}

// TableHeaders implements format.TableMarshaler.
func (r *COVRecord) TableHeaders() []string { return []string{"TIME", "OBJECT", "PROPERTY", "VALUE"} }

// TableRows implements format.TableMarshaler.
func (r *COVRecord) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Values))
	for _, v := range r.Values {
		rows = append(rows, []string{clock(r.Timestamp), fmt.Sprintf("%-18s", r.Object), fmt.Sprintf("%-14s", v.Label()), v.Value.String()})
	}
	return rows
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *COVRecord) MarshalTextOutput() (string, error) {
	parts := make([]string, 0, len(r.Values))
	for _, v := range r.Values {
		parts = append(parts, v.Label()+"="+v.Value.String())
	}
	gap := ""
	if r.Gap {
		gap = "  (notifications were lost before this one)"
	}
	return fmt.Sprintf("%s  %-18s  %s%s", clock(r.Timestamp), r.Object, strings.Join(parts, "  "), gap), nil
}

// MonitorSummary is what `monitor` and `events` report on stderr when they end.
type MonitorSummary struct {
	Target        string
	Subscriptions int
	Received      int
	Duration      float64
	What          string
}

// String implements fmt.Stringer.
func (s MonitorSummary) String() string {
	return fmt.Sprintf("%s: %s in %s", s.Target, plural(s.Received, s.What, s.What+"s"), seconds(s.Duration))
}

// ---- alarms / ack / events ----

// Alarm is one object that is in an alarm state or has unacknowledged transitions.
type Alarm struct {
	ObjectRef
	EventState   string `json:"event_state"`
	EventStateID uint32 `json:"event_state_id"`
	// Unacknowledged are the transitions that wait for an acknowledgement.
	Unacknowledged []string `json:"unacknowledged"`
	NotifyType     string   `json:"notify_type,omitempty"`
	NotifyTypeID   *uint32  `json:"notify_type_id,omitempty"`
	// EventEnable are the transitions the object reports.
	EventEnable []string `json:"event_enable,omitempty"`
	// TimeStamps are the times of the last to-offnormal, to-fault and to-normal
	// transitions, as text.
	TimeStamps []string `json:"time_stamps,omitempty"`
	// Priorities are the notification priorities of those three transitions.
	Priorities []uint64 `json:"priorities,omitempty"`
}

// AlarmsResult is the output of `alarms`.
type AlarmsResult struct {
	Target string `json:"target"`
	// Service is "get-event-information" or "get-alarm-summary".
	Service string  `json:"service"`
	Count   int     `json:"count"`
	Alarms  []Alarm `json:"alarms"`
}

// TableHeaders implements format.TableMarshaler.
func (r *AlarmsResult) TableHeaders() []string {
	return []string{"OBJECT", "STATE", "UNACKNOWLEDGED", "NOTIFY TYPE", "LAST TRANSITIONS"}
}

// TableRows implements format.TableMarshaler.
func (r *AlarmsResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Alarms))
	for _, a := range r.Alarms {
		rows = append(rows, []string{a.Object, a.EventState, dash(strings.Join(a.Unacknowledged, ",")), dash(a.NotifyType), dash(strings.Join(a.TimeStamps, " | "))})
	}
	return rows
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *AlarmsResult) MarshalTextOutput() (string, error) {
	summary := fmt.Sprintf("%s at %s (%s)", plural(r.Count, "active event", "active events"), r.Target, r.Service)
	return listText(summary, r.TableHeaders(), r.TableRows()), nil
}

// AckResult is the output of `ack`.
type AckResult struct {
	Target string `json:"target"`
	ObjectRef
	State     string `json:"state"`
	StateID   uint32 `json:"state_id"`
	TimeStamp string `json:"time_stamp"`
	Source    string `json:"source"`
	// Status is "acknowledged".
	Status string  `json:"status"`
	RTTMS  float64 `json:"rtt_ms"`
}

// TableHeaders implements format.TableMarshaler.
func (r *AckResult) TableHeaders() []string {
	return []string{"OBJECT", "STATE", "TIME STAMP", "SOURCE", "STATUS"}
}

// TableRows implements format.TableMarshaler.
func (r *AckResult) TableRows() [][]string {
	return [][]string{{r.Object, r.State, r.TimeStamp, r.Source, r.Status}}
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *AckResult) MarshalTextOutput() (string, error) {
	return fmt.Sprintf("%s: %s transition of %s acknowledged as %q", r.Object, r.State, r.TimeStamp, r.Source), nil
}

// EventRecord is one event notification.
type EventRecord struct {
	// Timestamp is when the notification arrived, RFC 3339 with milliseconds, UTC.
	Timestamp string `json:"timestamp"`
	// From is the BACnet/IP address the notification came from.
	From      string `json:"from"`
	Confirmed bool   `json:"confirmed"`
	Device    string `json:"device"`
	ObjectRef
	EventType         string `json:"event_type"`
	EventTypeID       uint32 `json:"event_type_id"`
	NotifyType        string `json:"notify_type"`
	NotifyTypeID      uint32 `json:"notify_type_id"`
	FromState         string `json:"from_state,omitempty"`
	ToState           string `json:"to_state"`
	ToStateID         uint32 `json:"to_state_id"`
	Priority          uint8  `json:"priority"`
	NotificationClass uint32 `json:"notification_class"`
	AckRequired       *bool  `json:"ack_required,omitempty"`
	Message           string `json:"message,omitempty"`
	// EventTime is the time stamp the device gave the event: what `ack` needs.
	EventTime string `json:"event_time"`
	Gap       bool   `json:"gap,omitempty"`
}

// TableHeaders implements format.TableMarshaler.
func (r *EventRecord) TableHeaders() []string {
	return []string{"TIME", "DEVICE", "OBJECT", "TYPE", "TRANSITION", "PRIORITY", "MESSAGE"}
}

func (r *EventRecord) transition() string {
	if r.FromState == "" {
		return r.ToState
	}
	return r.FromState + " -> " + r.ToState
}

// TableRows implements format.TableMarshaler.
func (r *EventRecord) TableRows() [][]string {
	return [][]string{{clock(r.Timestamp), fmt.Sprintf("%-14s", r.Device), fmt.Sprintf("%-18s", r.Object),
		fmt.Sprintf("%-16s", r.EventType), fmt.Sprintf("%-22s", r.transition()), strconv.Itoa(int(r.Priority)), dash(r.Message)}}
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *EventRecord) MarshalTextOutput() (string, error) {
	extra := ""
	if r.AckRequired != nil && *r.AckRequired {
		extra += "  ack required"
	}
	if r.Message != "" {
		extra += "  " + strconv.Quote(r.Message)
	}
	return fmt.Sprintf("%s  %s %s  %s %s: %s  priority %d  event time %s%s",
		clock(r.Timestamp), r.Device, r.Object, r.NotifyType, r.EventType, r.transition(), r.Priority, r.EventTime, extra), nil
}

// ---- trend ----

// LogRecord is one record of a trend log.
type LogRecord struct {
	// Position is the place of the record in the buffer, counted from 1.
	Position uint32 `json:"position"`
	// Timestamp is the time the device logged the value, in its own local time.
	Timestamp string `json:"timestamp"`
	// Kind is "value", "status" (a change of the log status), "failure" (the logged
	// property could not be read) or "time-change".
	Kind        string   `json:"kind"`
	Value       *Value   `json:"value,omitempty"`
	StatusFlags []string `json:"status_flags,omitempty"`
}

// MarshalJSON implements json.Marshaler. The status flags of a record that carries them
// are always there, also when none is set.
func (r LogRecord) MarshalJSON() ([]byte, error) {
	type plain LogRecord
	out := struct {
		plain
		StatusFlags *[]string `json:"status_flags,omitempty"`
	}{plain: plain(r)}
	if r.StatusFlags != nil {
		out.StatusFlags = &r.StatusFlags
	}
	return json.Marshal(out)
}

// TrendResult is the output of `trend`.
type TrendResult struct {
	Target string `json:"target"`
	ObjectRef
	// Total is the number of records in the buffer, when the device says.
	Total   *uint32     `json:"total,omitempty"`
	Start   uint32      `json:"start"`
	Count   int         `json:"count"`
	Partial bool        `json:"partial,omitempty"`
	Records []LogRecord `json:"records"`
}

// TableHeaders implements format.TableMarshaler.
func (r *TrendResult) TableHeaders() []string {
	return []string{"POSITION", "TIMESTAMP", "KIND", "VALUE", "STATUS"}
}

// TableRows implements format.TableMarshaler.
func (r *TrendResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Records))
	for _, rec := range r.Records {
		rows = append(rows, []string{strconv.FormatUint(uint64(rec.Position), 10), rec.Timestamp, rec.Kind, TextOf(rec.Value), flagsText(rec.StatusFlags)})
	}
	return rows
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *TrendResult) MarshalTextOutput() (string, error) {
	summary := fmt.Sprintf("%s at %s: %s from position %d", r.Object, r.Target, plural(r.Count, "record", "records"), r.Start)
	if r.Total != nil {
		summary += fmt.Sprintf(" (%d in the buffer)", *r.Total)
	}
	if r.Partial {
		summary += ", more follow"
	}
	return listText(summary, r.TableHeaders(), r.TableRows()), nil
}

// ---- file ----

// FileResult is the output of `file get` and `file put`.
type FileResult struct {
	Target string `json:"target"`
	// Direction is "download" or "upload".
	Direction string `json:"direction"`
	ObjectRef
	Length     int     `json:"length"`
	SHA256     string  `json:"sha256"`
	Path       string  `json:"path"`
	Requests   int     `json:"requests,omitempty"`
	DurationMS float64 `json:"duration_ms"`
}

// TableHeaders implements format.TableMarshaler.
func (r *FileResult) TableHeaders() []string {
	return []string{"DIRECTION", "OBJECT", "LENGTH", "SHA256", "PATH"}
}

// TableRows implements format.TableMarshaler.
func (r *FileResult) TableRows() [][]string {
	return [][]string{{r.Direction, r.Object, strconv.Itoa(r.Length), r.SHA256, r.Path}}
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *FileResult) MarshalTextOutput() (string, error) {
	verb, prep, path := "read", "to", r.Path
	if r.Direction == "upload" {
		verb, prep = "written", "from"
	} else if path == "-" {
		path = "stdout"
	}
	return fmt.Sprintf("%s: %d octets %s %s %s in %s\nsha256: %s", r.Object, r.Length, verb, prep, path, seconds(r.DurationMS), r.SHA256), nil
}

// ---- time sync ----

// TimeSyncResult is the output of `time sync`.
type TimeSyncResult struct {
	// Target is the device, or "broadcast".
	Target string `json:"target"`
	// Service is "time-synchronization" or "utc-time-synchronization".
	Service string `json:"service"`
	// Time is the date and time that was sent, as it was sent: local or UTC.
	Time string `json:"time"`
	// Status is "sent": the service is unconfirmed, a device does not answer it.
	Status string `json:"status"`
}

// TableHeaders implements format.TableMarshaler.
func (r *TimeSyncResult) TableHeaders() []string {
	return []string{"TARGET", "SERVICE", "TIME", "STATUS"}
}

// TableRows implements format.TableMarshaler.
func (r *TimeSyncResult) TableRows() [][]string {
	return [][]string{{r.Target, r.Service, r.Time, r.Status}}
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *TimeSyncResult) MarshalTextOutput() (string, error) {
	return fmt.Sprintf("%s sent to %s: %s (unconfirmed: the device does not answer)", r.Service, r.Target, r.Time), nil
}

// ---- router / bbmd ----

// Router is one route: a BACnet network and the router that leads to it.
type Router struct {
	Network uint16 `json:"network"`
	Router  string `json:"router"`
	// State is "available", "busy", "rejected" or "expired".
	State string `json:"state"`
}

// RoutersResult is the output of `router list`.
type RoutersResult struct {
	Mode       string   `json:"mode"`
	Target     string   `json:"target,omitempty"`
	DurationMS float64  `json:"duration_ms"`
	Count      int      `json:"count"`
	Routers    []Router `json:"routers"`
}

// TableHeaders implements format.TableMarshaler.
func (r *RoutersResult) TableHeaders() []string { return []string{"NETWORK", "ROUTER", "STATE"} }

// TableRows implements format.TableMarshaler.
func (r *RoutersResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Routers))
	for _, x := range r.Routers {
		rows = append(rows, []string{strconv.Itoa(int(x.Network)), x.Router, x.State})
	}
	return rows
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *RoutersResult) MarshalTextOutput() (string, error) {
	summary := fmt.Sprintf("%s found in %s", plural(r.Count, "route", "routes"), seconds(r.DurationMS))
	return listText(summary, r.TableHeaders(), r.TableRows()), nil
}

// TableEntry is one entry of a broadcast distribution table or a foreign device table.
type TableEntry struct {
	Address string `json:"address"`
	// Mask is the broadcast distribution mask of a BDT entry.
	Mask string `json:"mask,omitempty"`
	// TTL and Remaining are the registration lifetime of an FDT entry, in seconds.
	TTL       *uint16 `json:"ttl,omitempty"`
	Remaining *uint16 `json:"remaining,omitempty"`
}

// BBMDTableResult is the output of `bbmd bdt` and `bbmd fdt`.
type BBMDTableResult struct {
	BBMD string `json:"bbmd"`
	// Table is "bdt" or "fdt".
	Table   string       `json:"table"`
	Count   int          `json:"count"`
	Entries []TableEntry `json:"entries"`
}

// TableHeaders implements format.TableMarshaler.
func (r *BBMDTableResult) TableHeaders() []string {
	if r.Table == "fdt" {
		return []string{"ADDRESS", "TTL", "REMAINING"}
	}
	return []string{"ADDRESS", "MASK"}
}

// TableRows implements format.TableMarshaler.
func (r *BBMDTableResult) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Entries))
	for _, e := range r.Entries {
		if r.Table == "fdt" {
			rows = append(rows, []string{e.Address, optUint(e.TTL) + " s", optUint(e.Remaining) + " s"})
		} else {
			rows = append(rows, []string{e.Address, e.Mask})
		}
	}
	return rows
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *BBMDTableResult) MarshalTextOutput() (string, error) {
	name := "broadcast distribution table"
	if r.Table == "fdt" {
		name = "foreign device table"
	}
	summary := fmt.Sprintf("%s of %s: %s", name, r.BBMD, plural(r.Count, "entry", "entries"))
	return listText(summary, r.TableHeaders(), r.TableRows()), nil
}

// ---- reinitialize / comm-control ----

// ManageResult is the output of `reinitialize` and `comm-control`.
type ManageResult struct {
	Target string `json:"target"`
	// Request is "reinitialize-device" or "device-communication-control".
	Request string `json:"request"`
	// State is what was asked for: coldstart, warmstart, enable, disable, ….
	State string `json:"state"`
	// DurationMinutes is the duration of a communication control, when one was given.
	DurationMinutes *uint16 `json:"duration_minutes,omitempty"`
	// Status is "acknowledged".
	Status string  `json:"status"`
	RTTMS  float64 `json:"rtt_ms"`
}

// TableHeaders implements format.TableMarshaler.
func (r *ManageResult) TableHeaders() []string {
	return []string{"TARGET", "REQUEST", "STATE", "STATUS"}
}

// TableRows implements format.TableMarshaler.
func (r *ManageResult) TableRows() [][]string {
	return [][]string{{r.Target, r.Request, r.State, r.Status}}
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *ManageResult) MarshalTextOutput() (string, error) {
	d := ""
	if r.DurationMinutes != nil {
		d = fmt.Sprintf(" for %d min", *r.DurationMinutes)
	}
	return fmt.Sprintf("%s: %s %s%s acknowledged by the device", r.Target, r.Request, r.State, d), nil
}

// ---- decode ----

// Field is one decoded field of a frame.
type Field struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

// Layer is one protocol layer of a decoded frame.
type Layer struct {
	// Layer is "bvlc", "npdu", "apdu" or "service".
	Layer string `json:"layer"`
	// Summary names what the layer carries: the BVLC function, the PDU type, the service.
	Summary string  `json:"summary"`
	Fields  []Field `json:"fields"`
}

// DecodeResult is the output of `decode`.
type DecodeResult struct {
	Length int     `json:"length"`
	Layers []Layer `json:"layers"`
	// Error is why decoding stopped before the end of the frame, when it did.
	Error string `json:"error,omitempty"`
}

// TableHeaders implements format.TableMarshaler.
func (r *DecodeResult) TableHeaders() []string { return []string{"LAYER", "FIELD", "VALUE"} }

// TableRows implements format.TableMarshaler.
func (r *DecodeResult) TableRows() [][]string {
	var rows [][]string
	for _, l := range r.Layers {
		rows = append(rows, []string{l.Layer, "", l.Summary})
		for _, f := range l.Fields {
			rows = append(rows, []string{"", f.Name, fmt.Sprint(f.Value)})
		}
	}
	if r.Error != "" {
		rows = append(rows, []string{"error", "", r.Error})
	}
	return rows
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *DecodeResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	for _, l := range r.Layers {
		_, _ = fmt.Fprintf(&b, "%-8s %s\n", strings.ToUpper(l.Layer), l.Summary)
		width := 0
		for _, f := range l.Fields {
			width = max(width, len(f.Name))
		}
		for _, f := range l.Fields {
			_, _ = fmt.Fprintf(&b, "  %-*s  %v\n", width, f.Name, f.Value)
		}
	}
	if r.Error != "" {
		_, _ = fmt.Fprintf(&b, "error: %s\n", r.Error)
	}
	return b.String(), nil
}

// ---- types / properties ----

// Named is one row of `types` or `properties`.
type Named struct {
	ID    uint32 `json:"id"`
	Name  string `json:"name"`
	Alias string `json:"alias,omitempty"`
}

// NameList is the output of `types` and `properties`.
type NameList struct {
	// What is "object-types" or "properties".
	What  string  `json:"what"`
	Count int     `json:"count"`
	Names []Named `json:"names"`
}

// TableHeaders implements format.TableMarshaler.
func (r *NameList) TableHeaders() []string { return []string{"ID", "NAME", "SHORT"} }

// TableRows implements format.TableMarshaler.
func (r *NameList) TableRows() [][]string {
	rows := make([][]string, 0, len(r.Names))
	for _, n := range r.Names {
		rows = append(rows, []string{strconv.FormatUint(uint64(n.ID), 10), n.Name, n.Alias})
	}
	return rows
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *NameList) MarshalTextOutput() (string, error) {
	return grid(r.TableHeaders(), r.TableRows()), nil
}
