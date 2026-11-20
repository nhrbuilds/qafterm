# qafterm

A small, opinionated Go library for embedding a real terminal in a GUI
application.

```go
import "github.com/nhrbuilds/qafterm"
```

qafterm does **not** implement terminal emulation. That part is done by
battle-tested libraries:

| Concern | Library |
|---|---|
| VT/xterm emulation (parser, screen, scrollback, alt screen, mouse, truecolor) | [charmbracelet/x/vt](https://github.com/charmbracelet/x) |
| Pseudo-terminal handling | [creack/pty](https://github.com/creack/pty) |

What qafterm adds is the layer a **GUI host** needs and no library provides:

- a `Session` that owns a shell in a PTY and pumps bytes in both directions;
- a renderer-facing **snapshot** (rows of styled cells) that never exposes
  upstream types, so the emulator can be swapped without breaking users;
- **damage reporting** — which rows changed since the last snapshot, so a
  host repaints only what it must;
- **scrollback-aware viewport** rendering (scroll up into history);
- **key and mouse encoding** that respects the terminal's own modes (for
  example application cursor keys).

Everything is pure Go and CGO-free.

## Install

```bash
go get github.com/nhrbuilds/qafterm
```

## Usage

```go
ses, err := qafterm.Start(qafterm.Config{
    Shell: "zsh",   // empty: $SHELL, then the user's login shell, then /bin/sh
    Dir:   "/path/to/project",
    Cols:  120,
    Rows:  30,
})
if err != nil {
    return err
}
defer ses.Close()

for {
    rows := ses.View(0)          // viewport rows (scroll up with View(n))
    changed := ses.Damage()      // row indices that changed since last call
    render(rows, changed)
    time.Sleep(16 * time.Millisecond)
}
```

Sending input:

```go
_ = ses.Key(qafterm.Rune('a'))              // printable
_ = ses.Key(qafterm.SpecialMod(qafterm.KeyUp, qafterm.ModCtrl)) // ctrl+up
_ = ses.Text("hello")                       // literal text
_ = ses.Paste("pasted")                     // bracketed paste when enabled
_ = ses.Mouse(qafterm.MouseEvent{
    Kind: qafterm.MousePress, Button: qafterm.MouseLeft, X: 3, Y: 4,
})
```

## API

| Symbol | Purpose |
|---|---|
| `Start(Config) (*Session, error)` | Launch a shell in a PTY. |
| `Session.Write/Key/Text/Paste/Mouse` | Send input to the shell. |
| `Session.Snapshot() []Row` | The visible screen as styled cells. |
| `Session.View(scroll int) []Row` | The viewport, scrolled up into history. |
| `Session.Damage() []int` | Screen rows changed since the last call. |
| `Session.Resize(cols, rows int) error` | Resize the terminal and inform the shell. |
| `Session.Title() string` | The last window title the program set. |
| `Session.AltScreen() bool` | Whether the program uses the alternate screen. |
| `Session.ScrollbackLen() int` | Number of buffered history lines. |
| `Session.Focus() / Blur()` | Focus events (mode 1004). |
| `Session.Done() <-chan struct{}` | Closed when the shell exits. |

### Types

`Cell` carries the grapheme cluster, its width, the foreground and background
`Color`, the SGR attributes (`Bold`, `Faint`, `Italic`, `Blink`, `Reverse`,
`Conceal`, `Strike`) and the `Underline` style.

`Color` is one of:

- `ColorDefault` — the terminal's own default color;
- `ColorIndexed` — one of the 256 palette colors;
- `ColorRGB` — a direct 24-bit color.

## Design notes

- **No upstream types leak.** The public API is defined here, so the emulator
  can be replaced later without a breaking change for users.
- **Damage is computed by the host layer.** Row fingerprints make it a single
  comparison per row; a host repaints only the rows that changed.
- **Keys go through the emulator.** The emulator owns the terminal's modes
  (application cursor keys, keypad mode, mouse reporting), so key encoding
  stays correct without duplicating that logic.

## Roadmap

- Reflow on resize (wrapping preserved lines) — currently the screen is
  padded/truncated like `xterm` without reflow.
- A ready-to-use renderer for common toolkits (GTK, Gio).
- Optional shell integration helpers (command marks, OSC 133).

## License

MIT — see [LICENSE](LICENSE).
