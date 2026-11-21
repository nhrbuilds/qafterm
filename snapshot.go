package qafterm

import (
	"hash/fnv"
	"image/color"

	uv "github.com/charmbracelet/ultraviolet"
)

// ColorKind describes how a cell color is defined.
type ColorKind uint8

const (
	// ColorDefault means the terminal's default foreground or background.
	ColorDefault ColorKind = iota
	// ColorIndexed means one of the 256 palette colors.
	ColorIndexed
	// ColorRGB means a direct 24-bit color.
	ColorRGB
)

// Color is a cell color in a form that does not depend on the emulator.
type Color struct {
	Kind    ColorKind
	Index   uint8 // palette index for ColorIndexed
	R, G, B uint8
}

// DefaultColor is the terminal's default color.
var DefaultColor = Color{Kind: ColorDefault}

// IndexedColor returns a palette color.
func IndexedColor(i int) Color {
	return Color{Kind: ColorIndexed, Index: uint8(i & 0xff)}
}

// RGBColor returns a direct color.
func RGBColor(r, g, b uint8) Color {
	return Color{Kind: ColorRGB, R: r, G: g, B: b}
}

// UnderlineStyle is the underline decoration of a cell.
type UnderlineStyle uint8

const (
	UnderlineNone UnderlineStyle = iota
	UnderlineSingle
	UnderlineDouble
	UnderlineCurly
	UnderlineDotted
	UnderlineDashed
)

// Cell is one character cell of the terminal screen.
type Cell struct {
	// Text is the grapheme cluster shown in the cell. It is empty for a
	// blank cell.
	Text string
	// Width is the number of columns the cell occupies (0, 1 or 2).
	Width int
	// FG and BG are the foreground and background colors.
	FG, BG Color
	// Bold, Faint, Italic, Blink, Reverse, Conceal and Strike mirror the
	// corresponding SGR attributes.
	Bold, Faint, Italic, Blink, Reverse, Conceal, Strike bool
	// Underline is the underline decoration.
	Underline UnderlineStyle
}

// Row is one line of the terminal screen.
type Row []Cell

// Snapshot returns the current viewport: the visible screen rows.
//
// The walk holds emuMu for its whole duration: CellAt and Scrollback hand
// out pointers into the emulator's live buffer memory, so reading their
// fields must exclude the output pump's Write (which mutates those cells).
// emuMu is always taken before s.mu — the same order the pump uses.
func (s *Session) Snapshot() []Row {
	s.emuMu.Lock()
	defer s.emuMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.screenRows()
}

// View returns the viewport rows scrolled up by the given number of lines.
// A scroll of zero shows the live screen; larger values replace the top of
// the screen with history lines. The result always has the terminal's
// height.
func (s *Session) View(scroll int) []Row {
	s.emuMu.Lock()
	defer s.emuMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	height := s.emu.Height()
	if scroll <= 0 {
		return s.screenRows()
	}
	history := s.emu.ScrollbackLen()
	take := scroll
	if take > history {
		take = history
	}
	if take > height {
		take = height
	}
	out := make([]Row, 0, height)
	for i := history - take; i < history; i++ {
		out = append(out, s.historyRow(i))
	}
	screen := s.screenRows()
	// Fill the rest of the viewport with the top of the screen.
	if len(out) < height && len(screen) > 0 {
		out = append(out, screen[:height-len(out)]...)
	}
	for len(out) < height {
		out = append(out, nil)
	}
	return out
}

// Damage reports which screen rows changed since the previous call to
// Snapshot or View. The returned indices are relative to the visible screen;
// a scrolled viewport should be repainted in full.
func (s *Session) Damage() []int {
	s.emuMu.Lock()
	defer s.emuMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := s.screenRows()
	height := s.emu.Height()
	if len(s.hashes) != height {
		// The screen was resized: everything must be repainted.
		s.hashes = make([]uint64, height)
		for i := range s.hashes {
			s.hashes[i] = 0
		}
	}
	var changed []int
	for i := 0; i < height && i < len(rows); i++ {
		h := hashRow(rows[i])
		if h != s.hashes[i] {
			changed = append(changed, i)
			s.hashes[i] = h
		}
	}
	return changed
}

// screenRows builds the visible screen rows.
func (s *Session) screenRows() []Row {
	width, height := s.emu.Width(), s.emu.Height()
	rows := make([]Row, 0, height)
	for y := 0; y < height; y++ {
		row := make(Row, 0, width)
		for x := 0; x < width; x++ {
			row = append(row, s.convertCell(s.emu.CellAt(x, y)))
		}
		rows = append(rows, row)
	}
	return rows
}

// historyRow builds one scrollback row.
func (s *Session) historyRow(index int) Row {
	sb := s.emu.Scrollback()
	if sb == nil {
		return nil
	}
	width := s.emu.Width()
	line := sb.Line(index)
	row := make(Row, 0, width)
	for x := 0; x < width; x++ {
		row = append(row, s.convertCell(line.At(x)))
	}
	return row
}

// convertCell maps an emulator cell onto qafterm's own representation.
func (s *Session) convertCell(c *uv.Cell) Cell {
	if c == nil {
		return Cell{}
	}
	cell := Cell{Text: c.Content, Width: c.Width}
	cell.FG = s.convertColor(c.Style.Fg)
	cell.BG = s.convertColor(c.Style.Bg)
	cell.Bold = c.Style.Attrs&uv.AttrBold != 0
	cell.Faint = c.Style.Attrs&uv.AttrFaint != 0
	cell.Italic = c.Style.Attrs&uv.AttrItalic != 0
	cell.Blink = c.Style.Attrs&uv.AttrBlink != 0
	cell.Reverse = c.Style.Attrs&uv.AttrReverse != 0
	cell.Conceal = c.Style.Attrs&uv.AttrConceal != 0
	cell.Strike = c.Style.Attrs&uv.AttrStrikethrough != 0
	switch c.Style.Underline {
	case uv.UnderlineSingle:
		cell.Underline = UnderlineSingle
	case uv.UnderlineDouble:
		cell.Underline = UnderlineDouble
	case uv.UnderlineCurly:
		cell.Underline = UnderlineCurly
	case uv.UnderlineDotted:
		cell.Underline = UnderlineDotted
	case uv.UnderlineDashed:
		cell.Underline = UnderlineDashed
	default:
		cell.Underline = UnderlineNone
	}
	return cell
}

// convertColor maps a Go color onto qafterm's color model. Palette colors are
// detected by matching the emulator's own palette, so a host can render them
// with a terminal palette instead of hard-coding RGB values.
func (s *Session) convertColor(c color.Color) Color {
	if c == nil {
		return DefaultColor
	}
	r, g, b, a := c.RGBA()
	if a == 0 {
		return DefaultColor
	}
	if idx, ok := s.paletteIndex(r, g, b); ok {
		return IndexedColor(idx)
	}
	return RGBColor(uint8(r>>8), uint8(g>>8), uint8(b>>8))
}

// colorKey packs an 8-bit color into a comparable key. Colors must not be
// compared as interface values: the same RGB triple can be held by different
// concrete types (color.RGBA and color.NRGBA), which never compare equal.
// The values must be reduced to 8 bits first — color.RGBA returns 16-bit
// premultiplied components.
func colorKey(r, g, b uint8) uint32 {
	return uint32(r)<<24 | uint32(g)<<16 | uint32(b)<<8 | 0xff
}

// paletteIndex reports the palette index of a color, when it is one. The
// lookup table is built once per session.
func (s *Session) paletteIndex(r, g, b uint32) (int, bool) {
	if s.palette == nil {
		s.palette = make(map[uint32]int, 256)
		for i := 0; i < 256; i++ {
			p := s.emu.IndexedColor(i)
			if p == nil {
				continue
			}
			pr, pg, pb, _ := p.RGBA()
			key := colorKey(uint8(pr>>8), uint8(pg>>8), uint8(pb>>8))
			if _, exists := s.palette[key]; !exists {
				s.palette[key] = i
			}
		}
	}
	idx, ok := s.palette[colorKey(uint8(r>>8), uint8(g>>8), uint8(b>>8))]
	return idx, ok
}

// hashRow fingerprints a row so damage detection is a single comparison.
func hashRow(row Row) uint64 {
	h := fnv.New64a()
	for _, c := range row {
		_, _ = h.Write([]byte(c.Text))
		var buf [8]byte
		buf[0] = byte(c.Width)
		buf[1] = byte(c.FG.Kind)
		buf[2] = c.FG.Index
		buf[3] = byte(c.BG.Kind)
		buf[4] = byte(c.FG.R)
		buf[5] = byte(c.FG.G)
		buf[6] = byte(c.FG.B)
		buf[7] = byte(c.BG.R)
		_, _ = h.Write(buf[:])
	}
	return h.Sum64()
}
