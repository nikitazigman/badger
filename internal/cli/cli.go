package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/nikitazigman/badger/internal/sqlite"
)

type ExitCode uint

const (
	ExitSuccess    ExitCode = 0
	ExitError      ExitCode = 1
	ExitUsageError ExitCode = 2
)

type Command string

const (
	InspectCommand Command = "inspect"
	PageCommand    Command = "page"
	HelpCommand    Command = "help"
)

func Run(args []string, out io.Writer, errOut io.Writer) ExitCode {
	if len(args) == 0 {
		writeUsageError(errOut, "missing command")
		return ExitUsageError
	}

	command := Command(args[0])
	switch command {
	case InspectCommand:
		path, err := parseInspectArgs(args)
		if err != nil {
			writeUsageError(errOut, err.Error())
			return ExitUsageError
		}

		inspector, err := sqlite.Open(path)
		if err != nil {
			writeRuntimeError(errOut, "failed to open database %q: %v", path, err)
			return ExitError
		}
		defer inspector.Close()

		dto, err := inspector.InspectDatabaseMetadata()
		if err != nil {
			writeRuntimeError(errOut, "inspect failed for %q: %v", path, err)
			return ExitError
		}

		writeResult(out, "Inspect", dto)
		return ExitSuccess
	case PageCommand:
		path, pageNumber, err := parsePageArgs(args)
		if err != nil {
			writeUsageError(errOut, err.Error())
			return ExitUsageError
		}

		inspector, err := sqlite.Open(path)
		if err != nil {
			writeRuntimeError(errOut, "failed to open database %q: %v", path, err)
			return ExitError
		}
		defer inspector.Close()

		dto, err := inspector.InspectPage(pageNumber)
		if err != nil {
			writeRuntimeError(errOut, "page inspect failed for %q page %d: %v", path, pageNumber, err)
			return ExitError
		}

		writeResult(out, "Page", dto)
		return ExitSuccess
	case HelpCommand:
		writeUsage(out)
		return ExitSuccess
	default:
		writeUsageError(errOut, fmt.Sprintf("unsupported command %q", command))
		return ExitUsageError
	}
}

func parseInspectArgs(args []string) (string, error) {
	if len(args) < 2 {
		return "", errors.New("inspect requires <file.db>")
	}

	if len(args) > 2 {
		return "", fmt.Errorf("inspect accepts exactly 1 argument, got %d", len(args)-1)
	}

	return args[1], nil
}

func parsePageArgs(args []string) (string, uint32, error) {
	if len(args) < 2 {
		return "", 0, errors.New("page requires <file.db>")
	}

	if len(args) < 3 {
		return "", 0, errors.New("page requires <file.db> <N>")
	}
	if len(args) > 3 {
		return "", 0, fmt.Errorf("page accepts exactly 2 arguments, got %d", len(args)-1)
	}

	path := args[1]
	rawPage := args[2]

	page, err := strconv.ParseUint(rawPage, 10, 32)
	if err != nil {
		return "", 0, fmt.Errorf("invalid page number %q", rawPage)
	}
	if page == 0 {
		return "", 0, errors.New("page number must be >= 1")
	}

	return path, uint32(page), nil
}

func writeResult(out io.Writer, label string, value any) {
	fmt.Fprintf(out, "Badger %s Result\n", label)
	fmt.Fprintf(out, "%s\n", renderValue(value))
}

func renderValue(value any) string {
	if value == nil {
		return "<nil>"
	}

	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}

	// Keep type context for empty structs and other minimal payloads.
	if string(payload) == "{}" || string(payload) == "null" {
		return fmt.Sprintf("%T %s", value, payload)
	}
	return string(payload)
}

func writeRuntimeError(errOut io.Writer, format string, args ...any) {
	fmt.Fprintf(errOut, "error: "+format+"\n", args...)
}

func writeUsageError(errOut io.Writer, message string) {
	fmt.Fprintf(errOut, "usage error: %s\n\n", message)
	writeUsage(errOut)
}

func writeUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  badger inspect <file.db>")
	fmt.Fprintln(w, "  badger page <file.db> <N>")
	fmt.Fprintln(w, "  badger help")
}
