// Package kitty speaks the slice of the kitty terminal graphics protocol
// herdr-resurrect needs: transmit PNGs quietly (a=t, q=2), create virtual
// placements (a=p, U=1), and render unicode-placeholder cells — U+10EEEE +
// row diacritics + the image id encoded as a 256-color foreground — as plain
// styled text. Placeholder cells are ordinary text, so bubbletea's renderer
// moves images around inside frames for free (verified by tools/puaprobe:
// the rune, the diacritic cluster, the raw SGR, and repaint diffs all pass
// through the cellbuf untouched).
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

// Icons holds the image ids assigned to each agent kind. Small is a 2x1-cell
// placement for one-line rows; Big is 2x2 for two-line rows.
type Icons struct {
	Small map[string]int
	Big   map[string]int
}

// Icon returns the image id for a kind at the given placement height (1 or 2
// rows). ok=false when the kind has no image or the terminal has no protocol.
func (ic Icons) Icon(kind string, rows int) (int, bool) {
	if ic.Small == nil {
		return 0, false
	}
	switch rows {
	case 2:
		id, ok := ic.Big[kind]
		return id, ok
	default:
		id, ok := ic.Small[kind]
		return id, ok
	}
}

// Setup transmits every logo quietly and creates its two virtual placements.
// Ids live at 33+ so the 38;5;<id> foreground encoding never collides with
// the SGR basic colors (0-15) or brights (16-31).
func Setup(w io.Writer, logo func(string) ([]byte, bool), kinds []string) Icons {
	ic := Icons{Small: map[string]int{}, Big: map[string]int{}}
	id := 33
	for _, k := range kinds {
		png, ok := logo(k)
		if !ok {
			continue
		}
		if id+1 > 255 {
			break
		}
		if err := TransmitPNG(w, id, png); err != nil {
			continue
		}
		VirtualPlacement(w, id, 2, 1)
		ic.Small[k] = id
		// a second id for the 2x2 placement: one image, two rects, no
		// ambiguity about which placement a placeholder cell binds to
		TransmitPNG(w, id+1, png)
		VirtualPlacement(w, id+1, 2, 2)
		ic.Big[k] = id + 1
		id += 2
	}
	return ic
}

// TransmitPNG sends a PNG with the given image id (32..255, so the id fits
// the 256-color placeholder encoding) in quiet mode with no placement.
// Chunked base64 per the spec.
func TransmitPNG(w io.Writer, id int, png []byte) error {
	if id < 32 || id > 255 {
		return fmt.Errorf("image id %d outside placeholder range 32..255", id)
	}
	writeChunks(w, fmt.Sprintf("a=t,f=100,i=%d,q=2", id), png)
	return nil
}

func writeChunks(w io.Writer, header string, png []byte) {
	b64 := base64.StdEncoding.EncodeToString(png)
	const chunk = 4096
	first := true
	for len(b64) > 0 {
		n := min(chunk, len(b64))
		m := 1
		if n == len(b64) {
			m = 0 // last chunk
		}
		h := ""
		if first {
			h = "\x1b_G" + header + ",m=" + fmt.Sprint(m) + ";"
			first = false
		} else {
			h = "\x1b_G;m=" + fmt.Sprint(m) + ";"
		}
		fmt.Fprint(w, h+b64[:n]+"\x1b\\")
		b64 = b64[n:]
	}
}

// VirtualPlacement creates an id-keyed virtual placement of c columns and
// r rows that placeholder cells in the text grid reference. The image is fit
// to the rectangle with its aspect ratio preserved (spec).
func VirtualPlacement(w io.Writer, id, cols, rows int) {
	fmt.Fprintf(w, "\x1b_Ga=p,U=1,i=%d,c=%d,r=%d,q=2\x1b\\", id, cols, rows)
}

// row/col diacritics from the spec's rowcolumn-diacritics table (values 1
// and 2 are all small icons need; the spec's own 2x2 and 2x3 examples use
// exactly these). Built from code points, not literal combining characters.
var diacritics = map[int]string{1: string(rune(0x305)), 2: string(rune(0x30D))}

const ph = string(rune(0x10EEEE))

// Placeholder renders the placeholder cells for an image id as styled text,
// one string per text row. Each row's first cell carries the row diacritic;
// cells to its right inherit row and column (spec's inheritance rules).
// Bubbletea carries the cluster through frames; the cluster is width 1 per
// cell, so lipgloss/table alignment holds.
func Placeholder(id, cols, rows int) []string {
	out := make([]string, 0, rows)
	for r := 1; r <= rows; r++ {
		var b strings.Builder
		fmt.Fprintf(&b, "\x1b[38;5;%dm", id)
		b.WriteString(ph + diacritics[r])
		for c := 1; c < cols; c++ {
			b.WriteString(ph)
		}
		b.WriteString("\x1b[39m")
		out = append(out, b.String())
	}
	return out
}

// DeleteAll returns an escape that drops every transmitted image and frees
// its data (quit path). Per spec: a=d alone deletes all visible images; the
// uppercase d=A variant also frees the stored pixel data.
func DeleteAll() string { return "\x1b_Ga=d,d=A\x1b\\" }
