package proc

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarizeFirstLine(t *testing.T) {
	execErr := errors.New("exit status 1")
	cases := []struct{ out, want string }{
		{"", "exit status 1"},
		{" \n\t\n", "exit status 1"},
		{"one", "one"},
		{"\n  one\ntwo\n", "one"},
		{"hint: first\nfatal: second", "hint: first"},
	}
	for _, c := range cases {
		if got := summarize(execErr, c.out, firstLine).Error(); got != c.want {
			t.Errorf("summarize(%q, firstLine) = %q, want %q", c.out, got, c.want)
		}
	}
}

func TestSummarizeDiagnosis(t *testing.T) {
	execErr := errors.New("exit status 128")
	cases := []struct{ out, want string }{
		{"", "exit status 128"},
		{"   \n\n", "exit status 128"},
		{"hint: a\nhint: b", "exit status 128"},
		{"Cloning\nfatal: bad\nmore", "fatal: bad"},
		{"x\n  error: e1 \nfatal: f", "error: e1"},
		{"a\nb\nhint: use --force\n", "b"},
		{"a\n  last  \n\n", "last"},
		{"fatal:no-space\nlast", "last"},
	}
	for _, c := range cases {
		if got := summarize(execErr, c.out, diagnosis).Error(); got != c.want {
			t.Errorf("summarize(%q, diagnosis) = %q, want %q", c.out, got, c.want)
		}
	}
}

func TestSummarizeKeepsExecError(t *testing.T) {
	execErr := errors.New("exec: not found")
	if got := summarize(execErr, "", diagnosis); got != execErr {
		t.Errorf("got %v, want the exec error itself", got)
	}
}

func TestDisplay(t *testing.T) {
	got := Display("git", "commit", "-m", "two words", "", `a"b`)
	want := `git commit -m "two words" "" "a\"b"`
	if got != want {
		t.Errorf("Display = %q, want %q", got, want)
	}
}

// The tests below run this test binary as the child process, so they work
// wherever the tests do. TestHelperProcess is the child's entry point.
const helperEnv = "PROC_TEST_HELPER"

func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		t.Skip("child process only")
	}
	wd, _ := os.Getwd()
	switch mode := os.Getenv("PROC_TEST_MODE"); mode {
	case "ok":
		fmt.Printf("out %s %s\n", filepath.Base(wd), os.Getenv("PROC_TEST_VALUE"))
		os.Exit(0)
	case "fail":
		fmt.Print("partial")
		fmt.Fprint(os.Stderr, "\nfirst problem\nhint: retry\nsecond problem\n")
		os.Exit(1)
	case "silent":
		os.Exit(3)
	default:
		fmt.Fprintf(os.Stderr, "unknown mode %q", mode)
		os.Exit(2)
	}
}

func helper(t *testing.T, mode string) (dir string, env []string, name string, args []string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "work")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	env = []string{helperEnv + "=1", "PROC_TEST_MODE=" + mode, "PROC_TEST_VALUE=v"}
	return dir, env, os.Args[0], []string{"-test.run=^TestHelperProcess$"}
}

func TestReadSuccess(t *testing.T) {
	dir, env, name, args := helper(t, "ok")
	out, err := Read(dir, env, name, args...)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "out work v\n") {
		t.Errorf("stdout = %q, want dir and env passed through", out)
	}
}

func TestReadFailureReportsFirstStderrLine(t *testing.T) {
	dir, env, name, args := helper(t, "fail")
	out, err := Read(dir, env, name, args...)
	if err == nil || err.Error() != "first problem" {
		t.Errorf("err = %v, want first stderr line", err)
	}
	if out != "partial" {
		t.Errorf("stdout = %q, want what was printed before failing", out)
	}
}

func TestReadFailureWithoutStderr(t *testing.T) {
	dir, env, name, args := helper(t, "silent")
	if _, err := Read(dir, env, name, args...); err == nil || err.Error() != "exit status 3" {
		t.Errorf("err = %v, want the exec error", err)
	}
}

func TestRunSuccess(t *testing.T) {
	dir, env, name, args := helper(t, "ok")
	r := Run(dir, env, name, args...)
	if r.Err != nil {
		t.Fatal(r.Err)
	}
	if r.Dir != dir || r.Cmd != Display(name, args...) || !strings.HasPrefix(r.Output, "out work v") {
		t.Errorf("result = %+v", r)
	}
}

func TestRunFailureReportsLastMeaningfulLine(t *testing.T) {
	dir, env, name, args := helper(t, "fail")
	r := Run(dir, env, name, args...)
	if r.Err == nil || r.Err.Error() != "second problem" {
		t.Errorf("err = %v, want the last non-hint line", r.Err)
	}
	if !strings.Contains(r.Output, "partial") || !strings.Contains(r.Output, "first problem") {
		t.Errorf("output = %q, want stdout and stderr combined", r.Output)
	}
}

func TestRunFailureWithoutOutput(t *testing.T) {
	dir, env, name, args := helper(t, "silent")
	if r := Run(dir, env, name, args...); r.Err == nil || r.Err.Error() != "exit status 3" {
		t.Errorf("err = %v, want the exec error", r.Err)
	}
}

func TestRunMissingBinary(t *testing.T) {
	r := Run(t.TempDir(), nil, "gitdeck-no-such-binary")
	if r.Err == nil || !strings.Contains(r.Err.Error(), "gitdeck-no-such-binary") {
		t.Errorf("err = %v, want the exec error naming the binary", r.Err)
	}
	if r.Cmd != "gitdeck-no-such-binary" {
		t.Errorf("cmd = %q", r.Cmd)
	}
}
