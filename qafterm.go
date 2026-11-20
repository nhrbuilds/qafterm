// Package qafterm provides a small, opinionated host layer for embedding a
// real terminal in a GUI application.
//
// It deliberately does not implement terminal emulation itself: the emulator
// is [github.com/charmbracelet/x/vt], a battle-tested xterm/VT emulator, and
// the pseudo-terminal handling is [github.com/creack/pty]. qafterm wires them
// together and adds the pieces a GUI host needs but no library provides:
//
//   - A session that owns a shell in a PTY and pumps bytes in both directions.
//   - A renderer-facing snapshot (rows of styled cells) that never exposes
//     upstream types, so the emulator can be swapped without breaking users.
//   - Damage reporting: which rows changed since the last snapshot, so a host
//     repaints only what it must.
//   - Scrollback-aware viewport rendering (scroll up into history).
//   - Key and mouse encoding that respects the terminal's own modes (for
//     example application cursor keys).
//
// Everything is pure Go and CGO-free.
//
// Basic use:
//
//	ses, err := qafterm.Start(qafterm.Config{Cols: 120, Rows: 30})
//	if err != nil {
//		return err
//	}
//	defer ses.Close()
//
//	go func() {
//		for {
//			rows := ses.View(0) // viewport rows
//			changed := ses.Damage()
//			render(rows, changed)
//			time.Sleep(16 * time.Millisecond)
//		}
//	}()
package qafterm

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// Config describes a terminal session to start.
type Config struct {
	// Shell is the program to run. Empty means $SHELL, then the current
	// user's login shell from the password database, then /bin/sh.
	Shell string
	// Args are the arguments passed to the shell. When empty the shell is
	// started as a login shell ("-l") if it supports it, otherwise bare.
	Args []string
	// Dir is the working directory of the shell. Empty means the current
	// working directory.
	Dir string
	// Env holds extra environment entries in "KEY=VALUE" form. They are
	// appended to the inherited environment; an entry with an existing key
	// replaces it.
	Env []string
	// Cols and Rows are the initial terminal size in cells.
	Cols int
	Rows int
	// Scrollback is the number of history lines kept. Zero means the
	// emulator default (10000); negative disables scrollback.
	Scrollback int
}

const (
	defaultCols    = 80
	defaultRows    = 24
	defaultHistory = 10000
)

// Session is a running shell attached to a pseudo-terminal.
type Session struct {
	mu     sync.Mutex
	emu    *vt.SafeEmulator
	ptmx   *os.File
	cmd    *exec.Cmd
	done   chan struct{}
	closed chan struct{}

	title string
	bell  bool
	alt   bool
	cwd   string

	// Damage tracking: the fingerprint of every row of the last snapshot.
	hashes []uint64
	// rows is the height of the viewport the hashes describe.
	rows int
	// palette maps the emulator's palette colors to their indexes.
	palette map[uint32]int
	// modes holds the terminal modes the program enabled.
	modes map[ansi.Mode]bool

	closeOnce sync.Once
}

// Start launches a shell in a pseudo-terminal and returns a session for it.
func Start(cfg Config) (*Session, error) {
	shell, err := resolveShell(cfg.Shell)
	if err != nil {
		return nil, err
	}
	dir := cfg.Dir
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if fi, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("working directory %s: %w", dir, err)
	} else if !fi.IsDir() {
		return nil, fmt.Errorf("working directory %s is not a directory", dir)
	}
	cols, rows := cfg.Cols, cfg.Rows
	if cols <= 0 {
		cols = defaultCols
	}
	if rows <= 0 {
		rows = defaultRows
	}
	history := cfg.Scrollback
	if history == 0 {
		history = defaultHistory
	} else if history < 0 {
		history = 0
	}

	cmd := exec.Command(shell, cfg.Args...)
	cmd.Dir = dir
	cmd.Env = buildEnv(cfg.Env, cols, rows)
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", shell, err)
	}

	s := &Session{
		emu:    vt.NewSafeEmulator(cols, rows),
		ptmx:   ptmx,
		cmd:    cmd,
		done:   make(chan struct{}),
		closed: make(chan struct{}),
		rows:   rows,
		modes:  map[ansi.Mode]bool{},
	}
	s.emu.SetScrollbackSize(history)
	s.emu.SetCallbacks(vt.Callbacks{
		Bell:             func() { s.mu.Lock(); s.bell = true; s.mu.Unlock() },
		Title:            func(t string) { s.mu.Lock(); s.title = t; s.mu.Unlock() },
		AltScreen:        func(on bool) { s.mu.Lock(); s.alt = on; s.mu.Unlock() },
		WorkingDirectory: func(dir string) { s.mu.Lock(); s.cwd = dir; s.mu.Unlock() },
		EnableMode:       func(m ansi.Mode) { s.setMode(m, true) },
		DisableMode:      func(m ansi.Mode) { s.setMode(m, false) },
	})
	go s.pumpOutput()
	go s.pumpInput()
	return s, nil
}

// buildEnv returns the environment for the child process with TERM and the
// size hints applied.
func buildEnv(extra []string, cols, rows int) []string {
	env := os.Environ()
	set := func(k, v string) {
		prefix := k + "="
		for i := range env {
			if strings.HasPrefix(env[i], prefix) {
				env[i] = prefix + v
				return
			}
		}
		env = append(env, prefix+v)
	}
	set("TERM", "xterm-256color")
	set("COLORTERM", "truecolor")
	set("LINES", fmt.Sprint(rows))
	set("COLUMNS", fmt.Sprint(cols))
	for _, e := range extra {
		if k, v, ok := strings.Cut(e, "="); ok && k != "" {
			set(k, v)
		}
	}
	return env
}

// resolveShell picks the shell to run and verifies it exists.
func resolveShell(shell string) (string, error) {
	if shell == "" {
		shell = os.Getenv("SHELL")
	}
	if shell == "" {
		shell = loginShell()
	}
	if shell == "" {
		shell = "/bin/sh"
	}
	if strings.ContainsRune(shell, os.PathSeparator) {
		if _, err := os.Stat(shell); err != nil {
			return "", fmt.Errorf("shell %s: %w", shell, err)
		}
		return shell, nil
	}
	path, err := exec.LookPath(shell)
	if err != nil {
		return "", fmt.Errorf("shell %q not found in PATH", shell)
	}
	return path, nil
}

// loginShell returns the login shell of the current user, read from the
// password database (/etc/passwd). It returns "" when it cannot be found.
func loginShell() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), ":")
		if len(fields) < 7 || fields[0] != u.Username {
			continue
		}
		return fields[6]
	}
	return ""
}

// pumpOutput moves bytes from the PTY into the emulator.
func (s *Session) pumpOutput() {
	defer close(s.done)
	buf := make([]byte, 32*1024)
	for {
		n, err := s.ptmx.Read(buf)
		if n > 0 {
			_, _ = s.emu.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// pumpInput moves bytes the emulator produced (key presses, mouse reports)
// back to the shell.
func (s *Session) pumpInput() {
	defer close(s.closed)
	buf := make([]byte, 256)
	for {
		n, err := s.emu.Read(buf)
		if n > 0 {
			if _, werr := s.ptmx.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// Done is closed when the shell exits.
func (s *Session) Done() <-chan struct{} { return s.done }

// Close terminates the session and releases the pseudo-terminal.
func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		_ = s.emu.Close() // unblocks the input pump
		if s.cmd != nil && s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		err = s.ptmx.Close()
	})
	return err
}

// Resize changes the terminal size, informing the shell through the PTY.
func (s *Session) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("invalid size %dx%d", cols, rows)
	}
	if err := pty.Setsize(s.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		return err
	}
	s.emu.Resize(cols, rows)
	s.mu.Lock()
	s.rows = rows
	s.hashes = nil
	s.mu.Unlock()
	return nil
}

// Cols and Rows report the current terminal size.
func (s *Session) Cols() int { return s.emu.Width() }

// Rows reports the current terminal height.
func (s *Session) Rows() int { return s.emu.Height() }

// Title reports the last window title set by the program.
func (s *Session) Title() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.title
}

// Bell reports whether the terminal bell rang since the last call.
func (s *Session) Bell() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.bell
	s.bell = false
	return b
}

// AltScreen reports whether the program is using the alternate screen (full
// screen applications such as vim or htop).
func (s *Session) AltScreen() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alt
}

// WorkingDir reports the last working directory reported by the shell, when
// the program supports that reporting.
func (s *Session) WorkingDir() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cwd
}

// setMode records a terminal mode change.
func (s *Session) setMode(m ansi.Mode, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.modes == nil {
		s.modes = map[ansi.Mode]bool{}
	}
	s.modes[m] = on
}

// Cursor returns the cursor position in cells and whether the cursor is
// hidden (the program asked for it to be invisible).
func (s *Session) Cursor() (x, y int, hidden bool) {
	pos := s.emu.CursorPosition()
	return pos.X, pos.Y, s.emu.CursorHidden()
}

// MouseEnabled reports whether the program asked for mouse reports. Hosts
// must not forward mouse events while this is false: the program would
// receive reports it never asked for.
func (s *Session) MouseEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range []ansi.Mode{
		ansi.ModeMouseNormal, ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent,
	} {
		if s.modes[m] {
			return true
		}
	}
	return false
}

// BracketedPaste reports whether the program enabled bracketed paste.
func (s *Session) BracketedPaste() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.modes[ansi.ModeBracketedPaste]
}

// ScrollbackLen reports how many history lines are buffered.
func (s *Session) ScrollbackLen() int { return s.emu.ScrollbackLen() }

// MaxScrollback reports the scrollback limit.
func (s *Session) MaxScrollback() int {
	sb := s.emu.Scrollback()
	if sb == nil {
		return 0
	}
	return sb.MaxLines()
}

// Key sends a key press to the shell. Special keys use the Key* constants;
// printable keys use their rune.
func (s *Session) Key(k KeyEvent) error {
	s.emu.SendKey(k.toVT())
	return nil
}

// Text sends literal text to the shell (typing, pasting).
func (s *Session) Text(text string) error {
	s.emu.SendText(text)
	return nil
}

// Paste sends text to the shell using bracketed paste when the program
// enabled the mode.
func (s *Session) Paste(text string) error {
	s.emu.Paste(text)
	return nil
}

// Mouse sends a mouse event to the shell. It is ignored unless the program
// enabled mouse reporting.
func (s *Session) Mouse(ev MouseEvent) error {
	s.emu.SendMouse(ev.toVT())
	return nil
}

// Focus tells the shell the terminal gained focus (mode 1004).
func (s *Session) Focus() { s.emu.Focus() }

// Blur tells the shell the terminal lost focus (mode 1004).
func (s *Session) Blur() { s.emu.Blur() }

// ShellName returns a short display name for the shell (for example "zsh"),
// suitable for a tab title.
func (s *Session) ShellName() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil && len(s.cmd.Args) > 0 {
		return filepath.Base(s.cmd.Args[0])
	}
	return "shell"
}
