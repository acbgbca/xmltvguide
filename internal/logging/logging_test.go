package logging_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/acbgbca/xmltvguide/internal/logging"
)

// captureOutput swaps the default slog handler so the test can read what was
// emitted. It restores the previous default on cleanup.
func captureOutput(t *testing.T, level string) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })

	logging.InitWithWriter(level, buf)
	return buf
}

func TestInit_InfoLevelSuppressesDebug(t *testing.T) {
	buf := captureOutput(t, "info")

	logging.Debug("debug-line")
	logging.Info("info-line")

	out := buf.String()
	if strings.Contains(out, "debug-line") {
		t.Errorf("expected debug to be suppressed at info level; output: %q", out)
	}
	if !strings.Contains(out, "info-line") {
		t.Errorf("expected info-line in output; got: %q", out)
	}
}

func TestInit_DebugLevelEmitsAllLevels(t *testing.T) {
	buf := captureOutput(t, "debug")

	logging.Debug("d")
	logging.Info("i")
	logging.Warn("w")
	logging.Error("e")

	out := buf.String()
	for _, want := range []string{"d", "i", "w", "e"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output at debug level; got: %q", want, out)
		}
	}
}

func TestInit_WarnLevelSuppressesDebugAndInfo(t *testing.T) {
	buf := captureOutput(t, "warn")

	logging.Debug("d-line")
	logging.Info("i-line")
	logging.Warn("w-line")
	logging.Error("e-line")

	out := buf.String()
	if strings.Contains(out, "d-line") {
		t.Errorf("debug should be suppressed at warn level; got: %q", out)
	}
	if strings.Contains(out, "i-line") {
		t.Errorf("info should be suppressed at warn level; got: %q", out)
	}
	if !strings.Contains(out, "w-line") {
		t.Errorf("warn missing from output: %q", out)
	}
	if !strings.Contains(out, "e-line") {
		t.Errorf("error missing from output: %q", out)
	}
}

func TestInit_ErrorLevelEmitsOnlyError(t *testing.T) {
	buf := captureOutput(t, "error")

	logging.Debug("d-line")
	logging.Info("i-line")
	logging.Warn("w-line")
	logging.Error("e-line")

	out := buf.String()
	if strings.Contains(out, "d-line") || strings.Contains(out, "i-line") || strings.Contains(out, "w-line") {
		t.Errorf("only error should be emitted at error level; got: %q", out)
	}
	if !strings.Contains(out, "e-line") {
		t.Errorf("error missing from output: %q", out)
	}
}

func TestInit_InvalidLevelFallsBackToInfoAndWarns(t *testing.T) {
	buf := captureOutput(t, "verbose")

	// The init call should have emitted a warning about the invalid level.
	if !strings.Contains(strings.ToLower(buf.String()), "invalid log level") {
		t.Errorf("expected init to warn about invalid level; got: %q", buf.String())
	}

	buf.Reset()

	logging.Debug("dbg")
	logging.Info("inf")
	if strings.Contains(buf.String(), "dbg") {
		t.Errorf("invalid level should fall back to info (debug suppressed); got: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "inf") {
		t.Errorf("invalid level should fall back to info (info emitted); got: %q", buf.String())
	}
}

func TestInit_CaseInsensitive(t *testing.T) {
	buf := captureOutput(t, "DEBUG")
	logging.Debug("dbg-line")
	if !strings.Contains(buf.String(), "dbg-line") {
		t.Errorf("DEBUG (uppercase) should be accepted; got: %q", buf.String())
	}
}
