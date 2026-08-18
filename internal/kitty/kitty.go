// Package kitty speaks the minimal slice of the kitty graphics protocol
// herdr-archive needs: transmit PNGs quietly, create virtual placements,
// and render placeholder cells as plain text (U+10EEEE with the image id
// encoded as a 256-color foreground). Placeholder cells are ordinary text,
// so bubbletea's renderer moves images around for free.
package kitty

import (
	"encoding/base64"
	"fmt"
	"io"
	"strings"

	"github.com/BourgeoisBear/rasterm"
)

// Capable reports whether this terminal speaks the kitty graphics protocol.
func Capable() bool {
	return rasterm.IsKittyCapable()
}

// TransmitPNG sends a PNG with the given image id (1..255, so the id fits
// the 256-color placeholder encoding) in quiet mode with no placement.
// Chunked base64 per the spec.
func TransmitPNG(w io.Writer, id int, png []byte) error {
	if id < 1 || id > 255 {
		return fmt.Errorf("image id %d outside placeholder range 1..255", id)
	}
	b64 := base64.StdEncoding.EncodeToString(png)
	const chunk = 4096
	first := true
	for len(b64) > 0 {
		n := len(b64)
		if n > chunk {
			n = chunk
		}
		m := 1
		if first && n < len(b64) {
			// more chunks follow
		}
		if n == len(b64) {
			m = 0
		}
		q := "q=2,"
		if first {
			fmt.Fprintf(w, "\x1b_Ga=t,f=100,i=%d,%sm=%d;%s\x1b\\", id, q, m, b64[:n])
			first = false
		} else {
			fmt.Fprintf(w, "\x1b_G;m=%d;%s\x1b\\", m, b64[:n])
		}
		b64 = b64[n:]
	}
	return nil
}

// VirtualPlacement creates an id-keyed virtual placement of c columns and
// r rows that placeholder cells in the text grid can reference.
func VirtualPlacement(w io.Writer, id, cols, rows int) {
	fmt.Fprintf(w, "\x1b_Ga=p,U=1,i=%d,c=%d,r=%d,q=2\x1b\\", id, cols, rows)
}

// PlaceholderRow renders placeholder cells for an image id via the U+10EEEE
// unicode-placeholder scheme. NOTE: bubbletea v2's cellbuf currently drops
// that rune from frames, so this is unused by the TUI — kept for the day
// upstream preserves it (it is the only way to pin images inside frames).
func PlaceholderRow(id, cols int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b[38;5;%dm", id)
	for i := 0; i < cols; i++ {
		b.WriteRune(0x10EEEE)
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// StripItem is one logo cell in the pre-program brand strip.
type StripItem struct {
	PNG        []byte
	Label      string // styled label printed after the image
	Cols, Rows int    // display size in cells (2x1 ≈ square)
}

// Strip prints brand images at the cursor (a=T transmit-and-display) with
// labels between them, as plain pre-TUI output. Works because kitty images
// scroll with text; the strip stays above the inline (non-alt-screen) TUI.
func Strip(w io.Writer, items []StripItem) error {
	for _, it := range items {
		b64 := base64.StdEncoding.EncodeToString(it.PNG)
		const chunk = 4096
		first := true
		for len(b64) > 0 {
			n := len(b64)
			if n > chunk {
				n = chunk
			}
			m := 1
			if n == len(b64) {
				m = 0
			}
			if first {
				fmt.Fprintf(w, "\x1b_Ga=T,f=100,q=2,c=%d,r=%d,m=%d;%s\x1b\\", it.Cols, it.Rows, m, b64[:n])
				first = false
			} else {
				fmt.Fprintf(w, "\x1b_Gm=%d;%s\x1b\\", m, b64[:n])
			}
			b64 = b64[n:]
		}
		// advance past the image cells, then the label
		fmt.Fprintf(w, "%s%s", strings.Repeat(" ", it.Cols), it.Label)
	}
	fmt.Fprintln(w)
	return nil
}

// SetupString transmits the pngs and creates 2x1 virtual placements for
// each, returning the escape string to hand to tea.Raw and the assigned ids.
func SetupString(pngs [][]byte) (string, []int) {
	var b strings.Builder
	var ids []int
	for i, png := range pngs {
		id := i + 1
		if err := TransmitPNG(&b, id, png); err != nil {
			continue
		}
		VirtualPlacement(&b, id, 2, 1)
		ids = append(ids, id)
	}
	return b.String(), ids
}

// DeleteAll returns an escape that drops every transmitted image (quit path).
func DeleteAll() string { return "\x1b_Ga=d,A\x1b\\" }
