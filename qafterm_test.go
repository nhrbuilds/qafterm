package qafterm

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// startTestShell starts a session running a deterministic shell script
// instead of an interactive login shell.
func startTestShell(t *testing.T, script string) *Session {
	t.Helper()
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no POSIX shell available")
	}
	ses, err := Start(Config{
		Shell: "sh",
		Args:  []string{"-c", script},
		Cols:  40,
		Rows:  6,
	})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = ses.Close() })
	return ses
}

// waitFor polls until cond is true or the timeout expires.
func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return cond()
}

// rowText returns the visible text of a row.
func rowText(row Row) string {
	var b strings.Builder
	for _, c := range row {
		if c.Text != "" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}

// screenText returns the whole visible screen as text.
func screenText(ses *Session) string {
	rows := ses.Snapshot()
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(rowText(r))
		b.WriteString("\n")
	}
	return b.String()
}

func TestSessionRunsShellAndCapturesOutput(t *testing.T) {
	ses := startTestShell(t, "printf 'hello qafterm\\n'; sleep 5")
	if !waitFor(t, 3*time.Second, func() bool {
		return strings.Contains(screenText(ses), "hello qafterm")
	}) {
		t.Fatalf("output not captured, screen:\n%s", screenText(ses))
	}
}

func TestSessionColorsAndStyles(t *testing.T) {
	ses := startTestShell(t, "printf '\\033[31mRED\\033[0m\\n'; sleep 5")
	if !waitFor(t, 3*time.Second, func() bool {
		return strings.Contains(screenText(ses), "RED")
	}) {
		t.Fatalf("colored output missing, screen:\n%s", screenText(ses))
	}
	rows := ses.Snapshot()
	var found bool
	for _, row := range rows {
		for _, c := range row {
			if c.Text != "R" {
				continue
			}
			found = true
			if c.FG.Kind != ColorIndexed {
				t.Fatalf("expected indexed foreground, got %+v", c.FG)
			}
			if c.FG.Index != 1 { // xterm red is index 1
				t.Fatalf("expected red (index 1), got %+v", c.FG)
			}
		}
	}
	if !found {
		t.Fatal("no cell with text R found")
	}
}

func TestSessionBoldAttribute(t *testing.T) {
	ses := startTestShell(t, "printf '\\033[1mBOLD\\033[0m\\n'; sleep 5")
	if !waitFor(t, 3*time.Second, func() bool {
		return strings.Contains(screenText(ses), "BOLD")
	}) {
		t.Fatalf("bold output missing, screen:\n%s", screenText(ses))
	}
	for _, row := range ses.Snapshot() {
		for _, c := range row {
			if c.Text == "B" && !c.Bold {
				t.Fatalf("expected bold cell, got %+v", c)
			}
		}
	}
}

func TestSessionTitle(t *testing.T) {
	ses := startTestShell(t, "printf '\\033]0;my title\\007'; sleep 5")
	if !waitFor(t, 3*time.Second, func() bool { return ses.Title() == "my title" }) {
		t.Fatalf("title = %q", ses.Title())
	}
}

func TestSessionAltScreen(t *testing.T) {
	ses := startTestShell(t, "printf '\\033[?1049h'; sleep 5")
	if !waitFor(t, 3*time.Second, func() bool { return ses.AltScreen() }) {
		t.Fatal("alternate screen was not entered")
	}
}

func TestSessionScrollback(t *testing.T) {
	ses := startTestShell(t, "for i in $(seq 1 40); do echo line$i; done; sleep 5")
	if !waitFor(t, 5*time.Second, func() bool { return ses.ScrollbackLen() > 5 }) {
		t.Fatalf("scrollback did not grow: %d", ses.ScrollbackLen())
	}
	// Scrolling up shows history lines in the viewport.
	view := ses.View(3)
	if len(view) != ses.Rows() {
		t.Fatalf("viewport height = %d, want %d", len(view), ses.Rows())
	}
	joined := ""
	for _, r := range view {
		joined += rowText(r) + "\n"
	}
	if !strings.Contains(joined, "line") {
		t.Fatalf("scrolled view has no history:\n%s", joined)
	}
}

func TestSessionDamageReportsChangedRows(t *testing.T) {
	ses := startTestShell(t, "printf 'first\\n'; sleep 0.3; printf 'second\\n'; sleep 5")
	if !waitFor(t, 3*time.Second, func() bool {
		return strings.Contains(screenText(ses), "first")
	}) {
		t.Fatalf("first line missing:\n%s", screenText(ses))
	}
	// Draining the damage must eventually report the row that changed.
	changed := false
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, idx := range ses.Damage() {
			if idx == 1 {
				changed = true
			}
		}
		if changed {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !changed {
		t.Fatal("damage never reported the changed row")
	}
}

func TestSessionResize(t *testing.T) {
	ses := startTestShell(t, "sleep 5")
	if ses.Cols() != 40 || ses.Rows() != 6 {
		t.Fatalf("initial size = %dx%d", ses.Cols(), ses.Rows())
	}
	if err := ses.Resize(100, 24); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if ses.Cols() != 100 || ses.Rows() != 24 {
		t.Fatalf("size after resize = %dx%d", ses.Cols(), ses.Rows())
	}
	if err := ses.Resize(0, 0); err == nil {
		t.Fatal("resize to zero must fail")
	}
}

func TestSessionKeyInput(t *testing.T) {
	// The shell echoes what it receives, so sending keys must show up.
	ses := startTestShell(t, "cat; sleep 5")
	if err := ses.Key(Rune('h')); err != nil {
		t.Fatal(err)
	}
	if err := ses.Key(Rune('i')); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 3*time.Second, func() bool {
		return strings.Contains(screenText(ses), "hi")
	}) {
		t.Fatalf("key input not delivered, screen:\n%s", screenText(ses))
	}
}

func TestSessionSpecialKeyInput(t *testing.T) {
	// The terminal expands tabs to spaces on screen, so the raw byte stream
	// is what matters: capture what the shell actually receives.
	out := t.TempDir() + "/captured.txt"
	ses := startTestShell(t, "cat > "+out+"; sleep 5")
	time.Sleep(300 * time.Millisecond)
	if err := ses.Key(Special(KeyTab)); err != nil {
		t.Fatal(err)
	}
	if err := ses.Key(Special(KeyEnter)); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 3*time.Second, func() bool {
		b, err := os.ReadFile(out)
		return err == nil && len(b) > 0
	}) {
		t.Fatal("shell received nothing")
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "\t") {
		t.Fatalf("tab byte not delivered, captured %q", string(b))
	}
	if !strings.Contains(string(b), "\n") {
		t.Fatalf("enter byte not delivered, captured %q", string(b))
	}
}

func TestSessionTextAndPaste(t *testing.T) {
	ses := startTestShell(t, "cat; sleep 5")
	if err := ses.Text("pasted"); err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 3*time.Second, func() bool {
		return strings.Contains(screenText(ses), "pasted")
	}) {
		t.Fatalf("text not delivered, screen:\n%s", screenText(ses))
	}
}

func TestSessionChildExitSignalsDone(t *testing.T) {
	ses := startTestShell(t, "exit 3")
	if !waitFor(t, 5*time.Second, func() bool {
		select {
		case <-ses.Done():
			return true
		default:
			return false
		}
	}) {
		t.Fatal("session did not report the child exit")
	}
}

func TestStartRejectsBadShellAndDir(t *testing.T) {
	if _, err := Start(Config{Shell: "definitely-not-a-real-shell-xyz"}); err == nil {
		t.Fatal("unknown shell must error")
	}
	if _, err := Start(Config{Shell: "sh", Dir: "/definitely/not/a/dir"}); err == nil {
		t.Fatal("missing working directory must error")
	}
}

func TestStartUsesCurrentDirectory(t *testing.T) {
	dir := t.TempDir()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(old) }()
	ses, err := Start(Config{Shell: "sh", Args: []string{"-c", "pwd; sleep 5"}, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	defer ses.Close()
	// The reported path may be a symlinked variant of the temp dir.
	if !waitFor(t, 3*time.Second, func() bool {
		return strings.Contains(screenText(ses), filepathBase(dir))
	}) {
		t.Fatalf("pwd output missing:\n%s", screenText(ses))
	}
}

// filepathBase is a tiny helper to avoid importing path/filepath in tests
// that only need the last element.
func filepathBase(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
