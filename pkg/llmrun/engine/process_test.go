package engine

import (
	"errors"
	"testing"
	"time"
)

// fakeCmd implements interface{ Wait() error } for testing the reaper.
type fakeCmd struct {
	waitCh chan struct{} // closed when Wait should return
	err    error        // error to return from Wait
}

func (f *fakeCmd) Wait() error {
	<-f.waitCh
	return f.err
}

// TestProcessReaper_DoneClosedOnExit is a regression test for the zombie
// process bug. Before the fix, no background goroutine called Wait() on the
// child process, leaving zombies when the server crashed unexpectedly.
// The fix spawns a reaper goroutine at launch time. This test verifies
// that Done() is closed and Err() is populated when the process exits.
func TestProcessReaper_DoneClosedOnExit(t *testing.T) {
	waitCh := make(chan struct{})
	exitErr := errors.New("signal: killed")

	handle := &processHandle{
		pid:    99999,
		cmd:    &fakeCmd{waitCh: waitCh, err: exitErr},
		cancel: func() {},
		done:   make(chan struct{}),
	}

	// Simulate the background reaper goroutine that Launch() starts.
	go func() {
		handle.waitErr = handle.cmd.Wait()
		close(handle.done)
	}()

	proc := &Process{Cmd: handle}

	// Done should not be closed yet.
	select {
	case <-proc.Done():
		t.Fatal("Done() closed before process exited")
	default:
	}

	// Simulate process exit.
	close(waitCh)

	// Done should close promptly.
	select {
	case <-proc.Done():
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("Done() not closed after process exited")
	}

	if proc.Err() != exitErr {
		t.Errorf("expected %v, got %v", exitErr, proc.Err())
	}
}

// TestProcessStop_AlreadyExited verifies that Stop() on an already-exited
// process cleans up without errors instead of hanging or panicking.
func TestProcessStop_AlreadyExited(t *testing.T) {
	waitCh := make(chan struct{})
	close(waitCh) // process already exited

	handle := &processHandle{
		pid:    99999,
		cmd:    &fakeCmd{waitCh: waitCh, err: nil},
		cancel: func() {},
		done:   make(chan struct{}),
	}

	// Run reaper.
	go func() {
		handle.waitErr = handle.cmd.Wait()
		close(handle.done)
	}()

	// Let reaper finish.
	<-handle.done

	proc := &Process{Cmd: handle}

	err := proc.Stop()
	if err != nil {
		t.Fatalf("Stop() on exited process should succeed, got: %v", err)
	}
}

// TestProcessWait_BlocksUntilExit verifies that Wait() blocks and then
// returns the exit error.
func TestProcessWait_BlocksUntilExit(t *testing.T) {
	waitCh := make(chan struct{})
	exitErr := errors.New("exit status 1")

	handle := &processHandle{
		pid:    99999,
		cmd:    &fakeCmd{waitCh: waitCh, err: exitErr},
		cancel: func() {},
		done:   make(chan struct{}),
	}

	go func() {
		handle.waitErr = handle.cmd.Wait()
		close(handle.done)
	}()

	proc := &Process{Cmd: handle}

	// Start Wait in a goroutine.
	result := make(chan error, 1)
	go func() {
		result <- proc.Wait()
	}()

	// Should not have returned yet.
	select {
	case <-result:
		t.Fatal("Wait() returned before process exited")
	case <-time.After(50 * time.Millisecond):
	}

	// Let the process exit.
	close(waitCh)

	select {
	case err := <-result:
		if err != exitErr {
			t.Errorf("expected %v, got %v", exitErr, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Wait() did not return after process exited")
	}
}
