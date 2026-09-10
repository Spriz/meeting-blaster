package singleton

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// holdEnv puts a re-executed copy of this test binary into "take the lock
// and wait to be killed" mode, which is the only way to observe what
// happens to a lock when the process holding it dies.
const holdEnv = "MEETING_BLASTER_SINGLETON_HOLD"

func TestMain(m *testing.M) {
	if path := os.Getenv(holdEnv); path != "" {
		holdLock(path)
	}
	os.Exit(m.Run())
}

func holdLock(path string) {
	if _, err := acquire(path); err != nil {
		os.Stderr.WriteString("child could not take the lock: " + err.Error() + "\n")
		os.Exit(1)
	}
	os.Stdout.WriteString("held\n")

	// The parent kills this process as soon as it reads the line above.
	// The sleep only bounds how long a stranded child can linger.
	time.Sleep(time.Minute)
	os.Exit(0)
}

func TestSecondAcquireIsRefused(t *testing.T) {
	path := lockPath(t)

	release, err := acquire(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer release()

	_, err = acquire(path)
	var running ErrAlreadyRunning
	if !errors.As(err, &running) {
		t.Fatalf("second acquire: got %v, want ErrAlreadyRunning", err)
	}
}

// Acquire keys the lock to the runtime directory rather than to a single
// global name, so a second login session - which gets its own runtime
// directory - still gets its own copy of the app.
func TestAcquireIsScopedToTheRuntimeDirectory(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	release, err := Acquire()
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer release()

	if _, err := Acquire(); !errors.As(err, new(ErrAlreadyRunning)) {
		t.Fatalf("Acquire in the same runtime dir: got %v, want ErrAlreadyRunning", err)
	}

	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	elsewhere, err := Acquire()
	if err != nil {
		t.Fatalf("Acquire in a second runtime dir: %v", err)
	}
	elsewhere()
}

func TestReleasedLockCanBeTakenAgain(t *testing.T) {
	path := lockPath(t)

	release, err := acquire(path)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	release()
	release() // release is documented as safe to call more than once

	again, err := acquire(path)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	again()
}

// The lock has to die with the process that holds it. A lock that outlives
// a crash is worse than no lock: the app would refuse to start for good,
// and say only that another copy is running.
func TestLockDiesWithTheProcessHoldingIt(t *testing.T) {
	path := lockPath(t)

	child := exec.Command(os.Args[0])
	child.Env = append(os.Environ(), holdEnv+"="+path)
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	child.Stderr = os.Stderr
	if err := child.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	t.Cleanup(func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	})

	held := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			held <- scanner.Text()
		}
		close(held)
	}()

	select {
	case line, ok := <-held:
		if !ok || line != "held" {
			t.Fatalf("child did not report taking the lock, got %q", line)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for the child to take the lock")
	}

	if release, err := acquire(path); err == nil {
		release()
		t.Fatal("took a lock the child was holding")
	}

	if err := child.Process.Kill(); err != nil {
		t.Fatalf("kill child: %v", err)
	}
	_ = child.Wait()

	release, err := acquire(path)
	if err != nil {
		t.Fatalf("lock outlived the process holding it: %v", err)
	}
	release()
}

func lockPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "meeting-blaster.lock")
}
