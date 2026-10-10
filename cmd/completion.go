// SPDX-License-Identifier: MIT

package cmd

import "github.com/spf13/cobra"

const (
	completionBash       = "bash"
	completionZsh        = "zsh"
	completionFish       = "fish"
	completionPowerShell = "powershell"
)

var completionShells = []string{completionBash, completionZsh, completionFish, completionPowerShell}

func newCompletionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion script",
		Long: `Output a completion script for the given shell.

  Bash:        source <(bacnetctl completion bash)
  Zsh:         source <(bacnetctl completion zsh)
  Fish:        bacnetctl completion fish | source
  PowerShell:  bacnetctl completion powershell | Out-String | Invoke-Expression

Flags with a fixed set of values (--format, --type, --state) complete to those values;
object arguments complete to the object types, property arguments to the property names.`,
		Example: `
  # Load the completion of the current bash session
  source <(bacnetctl completion bash)

  # Install it for zsh
  bacnetctl completion zsh > "${fpath[1]}/_bacnetctl"
`,
		ValidArgs: completionShells,
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, out := cmd.Root(), cmd.OutOrStdout()
			switch args[0] {
			case completionBash:
				return root.GenBashCompletionV2(out, true)
			case completionZsh:
				return root.GenZshCompletion(out)
			case completionFish:
				return root.GenFishCompletion(out, true)
			default:
				return root.GenPowerShellCompletion(out)
			}
		},
	}
}
