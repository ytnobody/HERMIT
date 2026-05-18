package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestCmdVersion_Default(t *testing.T) {
	orig := Version
	Version = "dev"
	t.Cleanup(func() { Version = orig })

	pr, pw, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw
	cmdVersion()
	pw.Close()
	os.Stdout = origOut

	var buf bytes.Buffer
	buf.ReadFrom(pr)
	out := buf.String()
	if !strings.Contains(out, "hermit") {
		t.Errorf("expected 'hermit' in output, got %q", out)
	}
	if !strings.Contains(out, "dev") {
		t.Errorf("expected 'dev' in output, got %q", out)
	}
}

func TestCmdVersion_Tagged(t *testing.T) {
	orig := Version
	Version = "v1.2.3"
	t.Cleanup(func() { Version = orig })

	pr, pw, _ := os.Pipe()
	origOut := os.Stdout
	os.Stdout = pw
	cmdVersion()
	pw.Close()
	os.Stdout = origOut

	var buf bytes.Buffer
	buf.ReadFrom(pr)
	out := buf.String()
	if !strings.Contains(out, "v1.2.3") {
		t.Errorf("expected 'v1.2.3' in output, got %q", out)
	}
}

func TestMainSwitch_Version(t *testing.T) {
	orig := Version
	Version = "v0.0.1"
	t.Cleanup(func() { Version = orig })

	out := directMain(t, []string{"hermit", "version"})
	if !strings.Contains(out, "v0.0.1") {
		t.Errorf("expected version in main version output, got %q", out)
	}
}
