package lifecycle

import (
	"strings"
	"testing"
)

// A remediation line is only useful if the command it names exists. This one
// used to say `forget --force`, but forget takes --accept-orphan -- so an
// operator following it verbatim got "unknown flag: --force" while trying to
// clean up a failed bring-up.
func TestTeardownRemedy_NamesAFlagThatExists(t *testing.T) {
	err := unconfirmedTeardownError("upprobe")
	msg := err.Error()

	if strings.Contains(msg, "--force") {
		t.Errorf("remedy must not name --force; forget has no such flag: %s", msg)
	}
	if !strings.Contains(msg, "--accept-orphan") {
		t.Errorf("remedy should name the real flag (--accept-orphan): %s", msg)
	}
	if !strings.Contains(msg, "upprobe") {
		t.Errorf("remedy should name the instance so the command is copy-pasteable: %s", msg)
	}
}
