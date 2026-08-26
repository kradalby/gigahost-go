package cli

import (
	"strings"

	"github.com/peterbourgon/ff/v4"
)

// ff stops parsing flags at the first non-flag argument, the way Go's own
// flag package does. Every other CLI a user is likely to have in their hands —
// git, kubectl, gh, docker, tofu — permutes instead, so
//
//	gigahost dns zones create example.no --type NATIVE
//
// silently dropped --type and then failed with "exactly one ZONE_NAME argument
// is required", never mentioning the flag. Two subcommands were worse than
// that: `deploy status 123 -o json` sent "-o" and "json" to the API as order
// IDs, with no error at all.
//
// hoistFlags moves flags ahead of positionals before parsing, so both forms
// work. It is deliberately conservative: anything it cannot classify is left
// exactly where the user put it.

// takesValue reports whether a flag consumes the following argument. Boolean
// flags do not, so hoisting one must not drag an unrelated positional with it.
//
// Keep in step with the flag definitions; a flag missing here is merely not
// permuted, which is the behaviour users had before.
var takesValue = map[string]bool{
	// global
	"--token": true, "-t": true,
	"--username": true, "-u": true,
	"--password": true, "-p": true,
	"--base-url": true,
	"--output":   true, "-o": true,
	"--config": true, "-c": true,
	// dns
	"--zone": true, "-z": true,
	"--name": true, "-n": true,
	"--type":  true,
	"--value": true, "-V": true,
	"--ttl": true, "--priority": true,
	"--nameserver": true, "--source": true, "--target-url": true,
	"--key-tag": true, "--algorithm": true, "--digest-type": true, "--digest": true,
	"--prefix": true, "--version": true, "--zone-name": true,
	"--registrant-type": true, "--email": true, "-e": true,
	"--applicant-name": true, "--zip": true, "--city": true,
	"--org-number": true, "--company-name": true, "--pid": true,
	"--hostname": true, "-H": true,
	// servers
	"--ip": true, "--ip-id": true, "--subnet-id": true, "--dns": true, "-d": true,
	"--kind": true, "-w": true, "--output-file": true, "-O": true,
	"--acl": true, "--os": true, "--language": true, "--keyboard": true,
	"--timezone": true, "--server-id": true, "-s": true,
	// deploy
	"--size": true, "--region": true, "--ssh-keys": true, "--quantity": true,
	// account
	"--label": true, "-l": true, "--data": true, "--access": true, "-a": true,
	"--permissions": true, "--expires": true,
}

// hoistFlags returns args with every flag, and its value, moved ahead of the
// positional arguments — but only within the selected command's own
// arguments. The leading subcommand names must stay where they are, or ff
// cannot route to the command in the first place, so the command path is
// found by walking the real command tree rather than guessed.
//
// Order among flags and among positionals is preserved. A bare "--" ends flag
// processing, so a positional that genuinely starts with a dash stays
// possible. Anything unrecognised is left exactly where the user put it.
func hoistFlags(root *ff.Command, args []string) []string {
	split := commandPathLen(root, args)

	// The first argument names no subcommand, so this is not a command we
	// recognise. Reordering would turn a clear "unknown subcommand" into a
	// confusing "unknown flag"; leave it for the parser to reject as written.
	if split == 0 && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args
	}

	head, tail := args[:split], args[split:]

	flags := make([]string, 0, len(tail))
	positional := make([]string, 0, len(tail))

	for i := 0; i < len(tail); i++ {
		arg := tail[i]

		if arg == "--" {
			positional = append(positional, tail[i:]...)

			break
		}

		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)

			continue
		}

		flags = append(flags, arg)

		// "--flag=value" carries its own value; "--flag value" does not.
		if strings.Contains(arg, "=") || !takesValue[arg] {
			continue
		}

		if i+1 < len(tail) {
			flags = append(flags, tail[i+1])
			i++
		}
	}

	out := make([]string, 0, len(args))
	out = append(out, head...)
	out = append(out, flags...)
	out = append(out, positional...)

	return out
}

// commandPathLen returns how many leading arguments name subcommands, so the
// caller knows where the command path ends and its arguments begin.
func commandPathLen(cmd *ff.Command, args []string) int {
	n := 0

	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}

		next := findSubcommand(cmd, arg)
		if next == nil {
			break
		}

		cmd = next
		n++
	}

	return n
}

func findSubcommand(cmd *ff.Command, name string) *ff.Command {
	for _, sub := range cmd.Subcommands {
		if sub.Name == name {
			return sub
		}
	}

	return nil
}
