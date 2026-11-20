package qafterm

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

// Mod is a set of keyboard modifiers.
type Mod uint8

// Modifier bits.
const (
	ModShift Mod = 1 << iota
	ModAlt
	ModCtrl
	ModMeta
)

// Special key codes. Printable keys use their rune directly.
const (
	KeyEnter     = vt.KeyEnter
	KeyTab       = vt.KeyTab
	KeyEsc       = vt.KeyEscape
	KeyBackspace = vt.KeyBackspace
	KeySpace     = vt.KeySpace
	KeyUp        = vt.KeyUp
	KeyDown      = vt.KeyDown
	KeyRight     = vt.KeyRight
	KeyLeft      = vt.KeyLeft
	KeyHome      = vt.KeyHome
	KeyEnd       = vt.KeyEnd
	KeyPgUp      = vt.KeyPgUp
	KeyPgDown    = vt.KeyPgDown
	KeyInsert    = vt.KeyInsert
	KeyDelete    = vt.KeyDelete
	KeyF1        = vt.KeyF1
	KeyF2        = vt.KeyF2
	KeyF3        = vt.KeyF3
	KeyF4        = vt.KeyF4
	KeyF5        = vt.KeyF5
	KeyF6        = vt.KeyF6
	KeyF7        = vt.KeyF7
	KeyF8        = vt.KeyF8
	KeyF9        = vt.KeyF9
	KeyF10       = vt.KeyF10
	KeyF11       = vt.KeyF11
	KeyF12       = vt.KeyF12
)

// KeyEvent is a key press delivered to a session.
type KeyEvent struct {
	// Code is the key: a printable rune, or one of the Key* constants.
	Code rune
	// Text is the literal text the key produced, when it differs from Code
	// (for example a dead key or an IME result).
	Text string
	// Mod holds the active modifiers.
	Mod Mod
}

// toVT maps the event onto the emulator's key event. The emulator performs
// the actual escape-sequence encoding, including application cursor key mode.
func (k KeyEvent) toVT() vt.KeyPressEvent {
	ev := vt.KeyPressEvent{
		Code: k.Code,
		Text: k.Text,
	}
	if k.Mod&ModShift != 0 {
		ev.Mod |= uv.ModShift
	}
	if k.Mod&ModAlt != 0 {
		ev.Mod |= uv.ModAlt
	}
	if k.Mod&ModCtrl != 0 {
		ev.Mod |= uv.ModCtrl
	}
	if k.Mod&ModMeta != 0 {
		ev.Mod |= uv.ModMeta
	}
	return ev
}

// Rune builds a printable key event.
func Rune(r rune) KeyEvent { return KeyEvent{Code: r} }

// RuneMod builds a printable key event with modifiers.
func RuneMod(r rune, mod Mod) KeyEvent { return KeyEvent{Code: r, Mod: mod} }

// Special builds a key event for a special key.
func Special(code rune) KeyEvent { return KeyEvent{Code: code} }

// SpecialMod builds a key event for a special key with modifiers.
func SpecialMod(code rune, mod Mod) KeyEvent { return KeyEvent{Code: code, Mod: mod} }
