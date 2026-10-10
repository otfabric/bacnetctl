// SPDX-License-Identifier: MIT

package cli

import "github.com/spf13/cobra"

// Debug returns the root --debug flag value. Commands pass it into their configuration:
// with --debug the diagnostics of the BACnet client are logged to stderr.
func Debug(c *cobra.Command) bool {
	root := c.Root()
	if root == nil {
		return false
	}
	v, err := root.PersistentFlags().GetBool("debug")
	if err != nil {
		return false
	}
	return v
}
