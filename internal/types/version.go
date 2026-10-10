// SPDX-License-Identifier: MIT

package types

import (
	"fmt"
	"strings"
)

// VersionResult is the output of `version`.
type VersionResult struct {
	Version   string `json:"version"`
	Tag       string `json:"tag,omitempty"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// TableHeaders implements format.TableMarshaler.
func (r *VersionResult) TableHeaders() []string { return []string{"FIELD", "VALUE"} }

// TableRows implements format.TableMarshaler.
func (r *VersionResult) TableRows() [][]string {
	rows := [][]string{{"version", r.Version}}
	for _, kv := range [][2]string{{"tag", r.Tag}, {"commit", r.Commit}, {"build date", r.BuildDate}} {
		if kv[1] != "" {
			rows = append(rows, []string{kv[0], kv[1]})
		}
	}
	return append(rows, []string{"go version", r.GoVersion}, []string{"platform", r.OS + "/" + r.Arch})
}

// MarshalTextOutput implements format.TextMarshaler.
func (r *VersionResult) MarshalTextOutput() (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "bacnetctl version: %s\n", r.Version)
	for _, row := range r.TableRows()[1:] {
		fmt.Fprintf(&b, "%-12s %s\n", row[0]+":", row[1])
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
