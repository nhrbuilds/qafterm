package qafterm

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
)

// MouseKind describes the type of a mouse event.
type MouseKind uint8

const (
	// MousePress is a button press.
	MousePress MouseKind = iota
	// MouseRelease is a button release.
	MouseRelease
	// MouseMotion is a pointer move.
	MouseMotion
	// MouseWheel is a wheel movement.
	MouseWheel
)

// MouseButton identifies a mouse button.
type MouseButton uint8

// Mouse buttons, matching the xterm numbering.
const (
	MouseNone MouseButton = iota
	MouseLeft
	MouseMiddle
	MouseRight
	MouseWheelUp
	MouseWheelDown
	MouseWheelLeft
	MouseWheelRight
	MouseButton8
	MouseButton9
	MouseButton10
	MouseButton11
)

// MouseEvent is a mouse event delivered to a session.
type MouseEvent struct {
	Kind   MouseKind
	Button MouseButton
	// X and Y are the zero-based cell coordinates.
	X, Y int
	// Mod holds the active modifiers.
	Mod Mod
}

// toVT maps the event onto the emulator's mouse event, which encodes it
// according to the mouse mode the program enabled.
func (m MouseEvent) toVT() uv.MouseEvent {
	btn := ansi.MouseButton(m.Button)
	mod := uv.KeyMod(0)
	if m.Mod&ModShift != 0 {
		mod |= uv.ModShift
	}
	if m.Mod&ModAlt != 0 {
		mod |= uv.ModAlt
	}
	if m.Mod&ModCtrl != 0 {
		mod |= uv.ModCtrl
	}
	if m.Mod&ModMeta != 0 {
		mod |= uv.ModMeta
	}
	switch m.Kind {
	case MouseRelease:
		return uv.MouseReleaseEvent{X: m.X, Y: m.Y, Button: btn, Mod: mod}
	case MouseMotion:
		return uv.MouseMotionEvent{X: m.X, Y: m.Y, Button: btn, Mod: mod}
	case MouseWheel:
		return uv.MouseWheelEvent{X: m.X, Y: m.Y, Button: btn, Mod: mod}
	default:
		return uv.MouseClickEvent{X: m.X, Y: m.Y, Button: btn, Mod: mod}
	}
}
