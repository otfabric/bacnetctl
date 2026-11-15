// SPDX-License-Identifier: MIT

package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/otfabric/bacnetctl/internal/errs"
	"github.com/spf13/cobra"
)

// maxPasswordFile bounds what is read of a password file: far more than the 20 octets of
// a BACnet password, and little enough that a wrong path (a large file, a device) is not
// read whole.
const maxPasswordFile = 4096

// passwordFlags adds --password-file and the deprecated --password to cmd. what says whose
// password it is. env and fileEnv are the variables of the two; they are read when the
// command runs, never when its flags are defined, so that --help never shows a password.
func passwordFlags(cmd *cobra.Command, what, env, fileEnv string) {
	cmd.Flags().String("password-file", "", what+": read from this file (- = stdin), without the line break at its end; never printed [env: "+fileEnv+"]")
	// Not pflag's MarkDeprecated: it hides the flag from --help, and Cobra prints its notice
	// to the output writer, which is stdout and would break --format json.
	cmd.Flags().String("password", "", what+"; never printed [env: "+env+"] (deprecated: on the command line it is in the shell history and the process list; use --password-file or "+env+")")
}

// password returns the password of an invocation, from the first of: --password,
// --password-file, the variable env and the variable fileEnv. Both flags, or both
// variables, are refused: one of the two would be ignored without a word. --password
// still works, with a warning on stderr.
func password(cmd *cobra.Command, env, fileEnv string) (string, error) {
	flag, file := cmd.Flags().Lookup("password"), cmd.Flags().Lookup("password-file")
	switch {
	case flag.Changed && file.Changed:
		return "", fmt.Errorf("--password and --password-file both give the password: give one")
	case flag.Changed:
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: --password is deprecated: on the command line the password is in the shell history and the process list; use --password-file or %s\n", env)
		return flag.Value.String(), nil
	case file.Changed:
		return readPasswordFile(cmd, "--password-file", file.Value.String())
	}
	value, path := os.Getenv(env), os.Getenv(fileEnv)
	switch {
	case value != "" && path != "":
		return "", fmt.Errorf("%s and %s are both set: set one", env, fileEnv)
	case path != "":
		return readPasswordFile(cmd, fileEnv, path)
	default:
		return value, nil
	}
}

// readPasswordFile reads a password from path, or from stdin for "-". One line break (LF or
// CR LF) at the end is not part of the password. The content is never part of an error.
func readPasswordFile(cmd *cobra.Command, from, path string) (string, error) {
	r := cmd.InOrStdin()
	if path != "-" {
		f, err := os.Open(path) //nolint:gosec // a path the operator gave
		if err != nil {
			return "", errs.InvalidInput(errs.CodeInputFileOpenFailed, fmt.Sprintf("%s: cannot read the password file: %v", from, err), err)
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	data, err := io.ReadAll(io.LimitReader(r, maxPasswordFile+1))
	if err != nil {
		return "", errs.InvalidInput(errs.CodeInputFileOpenFailed, fmt.Sprintf("%s: cannot read the password file: %v", from, err), err)
	}
	if len(data) > maxPasswordFile {
		return "", errs.InvalidInput(errs.CodeInvalidInput, fmt.Sprintf("%s: the password file holds more than %d octets", from, maxPasswordFile), nil)
	}
	text := string(data)
	if t, ok := strings.CutSuffix(text, "\n"); ok {
		text = strings.TrimSuffix(t, "\r")
	}
	return text, nil
}
