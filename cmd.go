package rynek

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Cmd runs a shell pipeline with context cancellation. It is meant for tasks
// that wrap external tools (span, zstd, sort, solrbulk, ...), where the natural
// unit of work is a unix pipeline written as a string.
//
// The template may contain {name} placeholders that are replaced by the
// corresponding entry in args before execution. The rendered string is run via
// "bash -o pipefail -c", so a failure anywhere in a pipe fails the task. Stdout
// and stderr are forwarded to the process's own.
//
// Tasks that produce a file artifact should write to a temp path and rename
// into place (see Atomic), so a cancelled or crashed pipeline never leaves a
// half-written output that reads as complete.
func Cmd(ctx context.Context, tmpl string, args map[string]string) error {
	rendered := render(tmpl, args)
	c := exec.CommandContext(ctx, "bash", "-o", "pipefail", "-c", rendered)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("cmd failed: %s: %w", rendered, err)
	}
	return nil
}

// render substitutes {name} placeholders with args[name]. Unknown placeholders
// are left untouched so shell constructs like ${VAR} are unaffected.
func render(tmpl string, args map[string]string) string {
	if len(args) == 0 {
		return tmpl
	}
	oldnew := make([]string, 0, len(args)*2)
	for k, v := range args {
		oldnew = append(oldnew, "{"+k+"}", v)
	}
	return strings.NewReplacer(oldnew...).Replace(tmpl)
}
