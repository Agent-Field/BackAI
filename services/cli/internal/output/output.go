// SPDX-License-Identifier: Apache-2.0

// Package output is the CLI's single output + exit-code contract.
//
// Every af-stack command routes machine and human output through this
// package so the surface is stable enough for agents to drive:
//
//   - Stable exit codes (see the Exit* constants) let a script branch on
//     *why* a command failed without scraping stderr.
//   - Result() renders either a machine-readable JSON document (when the
//     caller passed --json) or a human table/text, from the same call site
//     — so a command can never drift its two representations apart.
//   - Fault carries an explicit exit code with an error, and ExitCode maps
//     any error (Fault, *client.APIError, flag.ErrHelp, or a plain error)
//     to the documented code. main.go calls os.Exit(ExitCode(err)).
//
// The exit-code table is documented for users in docs/cli-reference.md;
// keep the two in sync.
package output

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/Agent-Field/backai/services/cli/internal/client"
)

// Exit codes — the stable machine contract. Do not renumber; scripts and
// docs/cli-reference.md depend on these exact values.
const (
	ExitOK         = 0 // success
	ExitGeneric    = 1 // unclassified failure
	ExitUsage      = 2 // bad flags/args, unknown command (nothing ran)
	ExitAuth       = 3 // missing/invalid credentials, 401/403 from runtime
	ExitNotFound   = 4 // target does not exist, 404 from runtime
	ExitValidation = 5 // input failed validation, 400/409/422 from runtime
	ExitRemote     = 6 // runtime/API error or unreachable (5xx, transport)
)

// Fault is an error that carries an explicit CLI exit code. Commands return
// it (via the constructors below) when they want a code other than the
// generic 1.
type Fault struct {
	Code int
	Msg  string
	Err  error
}

func (f *Fault) Error() string {
	switch {
	case f.Msg != "" && f.Err != nil:
		return f.Msg + ": " + f.Err.Error()
	case f.Msg != "":
		return f.Msg
	case f.Err != nil:
		return f.Err.Error()
	default:
		return "af-stack: error"
	}
}

func (f *Fault) Unwrap() error { return f.Err }

func newFault(code int, format string, args ...any) *Fault {
	return &Fault{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// Usage reports a bad-invocation error (exit 2) — nothing was executed.
func Usage(format string, args ...any) *Fault { return newFault(ExitUsage, format, args...) }

// Auth reports missing/invalid credentials (exit 3).
func Auth(format string, args ...any) *Fault { return newFault(ExitAuth, format, args...) }

// NotFound reports a missing target (exit 4).
func NotFound(format string, args ...any) *Fault { return newFault(ExitNotFound, format, args...) }

// Invalid reports input that failed validation (exit 5).
func Invalid(format string, args ...any) *Fault { return newFault(ExitValidation, format, args...) }

// Remote reports a runtime/API failure or unreachable backend (exit 6).
func Remote(format string, args ...any) *Fault { return newFault(ExitRemote, format, args...) }

// Fail reports a generic failure (exit 1).
func Fail(format string, args ...any) *Fault { return newFault(ExitGeneric, format, args...) }

// Wrap attaches an exit code to an existing error while preserving it for
// errors.Is/As (e.g. a *client.APIError keeps unwrapping through the Fault).
func Wrap(code int, msg string, err error) *Fault {
	return &Fault{Code: code, Msg: msg, Err: err}
}

// ExitCode maps any error to its stable exit code. main.go uses it as the
// process exit status. The precedence is deliberate: an explicit Fault code
// wins, then a runtime *client.APIError status, then flag parsing, then the
// generic bucket.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	var f *Fault
	if errors.As(err, &f) {
		return f.Code
	}
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiStatusExit(apiErr.Status)
	}
	if errors.Is(err, flag.ErrHelp) {
		return ExitUsage
	}
	return ExitGeneric
}

// apiStatusExit maps an HTTP status from the runtime to a CLI exit code.
func apiStatusExit(status int) int {
	switch status {
	case 400, 409, 422:
		return ExitValidation
	case 401, 403:
		return ExitAuth
	case 404:
		return ExitNotFound
	default:
		// 402, 429, 5xx and anything else are treated as a remote/API
		// condition the caller could not have validated locally.
		return ExitRemote
	}
}

// EmitJSON writes v as indented JSON followed by a newline. This is the ONE
// place JSON is formatted so every --json document has the same shape.
func EmitJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return Fail("encode json: %v", err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// ParseArgs parses fs while allowing flags and positional arguments to be
// interspersed. Go's flag package stops at the first positional, which is
// hostile to agents that place --json (or --yes) after a name. This returns
// the positionals in order; read them instead of fs.Args() afterwards.
func ParseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positionals []string
	rest := args
	for {
		if err := fs.Parse(rest); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return positionals, nil
		}
		positionals = append(positionals, fs.Arg(0))
		rest = fs.Args()[1:]
	}
}

// Result renders a command's output in whichever representation the caller
// asked for: the machine document as JSON when asJSON is set, otherwise the
// human function's text. Both come from one call site so they cannot drift.
func Result(w io.Writer, asJSON bool, machine any, human func(io.Writer) error) error {
	if asJSON {
		return EmitJSON(w, machine)
	}
	return human(w)
}
