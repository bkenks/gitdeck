// Package proc runs external commands for gitdeck: detached from the
// terminal, with a caller-chosen environment, and with failures condensed to
// the one line worth showing in the footer.
package proc

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Result is the outcome of a command that changes something. The UI logs
// Cmd in its command log.
type Result struct {
	Dir    string
	Cmd    string
	Output string
	Err    error
}

// Read runs a command that only reads and returns its stdout. On failure the
// error is the first line of stderr, or the exec error when stderr is empty.
// env is added to the inherited environment.
func Read(dir string, env []string, name string, args ...string) (string, error) {
	cmd := command(dir, env, name, args)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return out.String(), summarize(err, errb.String(), firstLine)
	}
	return out.String(), nil
}

// Run runs a command that changes state and captures its combined output.
// On failure the error is the output's diagnosis line (see summarize). env is
// added to the inherited environment.
func Run(dir string, env []string, name string, args ...string) Result {
	out, err := command(dir, env, name, args).CombinedOutput()
	r := Result{Dir: dir, Cmd: Display(name, args...), Output: strings.TrimSpace(string(out))}
	if err != nil {
		r.Err = summarize(err, r.Output, diagnosis)
	}
	return r
}

// Display renders a command line for the command log.
func Display(name string, args ...string) string {
	parts := []string{name}
	for _, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\"'") {
			a = strconv.Quote(a)
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

func command(dir string, env []string, name string, args []string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	Detach(cmd)
	return cmd
}

// summarize restates a failed command's error as the line pick chooses from
// its output, keeping err itself when the output has nothing to offer.
func summarize(err error, output string, pick func(string) string) error {
	if line := pick(strings.TrimSpace(output)); line != "" {
		return errors.New(line)
	}
	return err
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// diagnosis picks the line of an action's output that explains a failure.
func diagnosis(s string) string {
	lines := strings.Split(s, "\n")
	// git's own diagnosis beats a wrapper's "exit status 128".
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "fatal: ") || strings.HasPrefix(l, "error: ") {
			return l
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" || strings.HasPrefix(l, "hint:") {
			continue
		}
		return l
	}
	return ""
}
