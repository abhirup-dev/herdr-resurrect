// logosmoke — run in a real kitty-graphics terminal (Ghostty/kitty): prints
// every embedded brand logo inline via unicode placeholders, with the kind
// name after each, exactly the way the browse TUI renders them. Also asserts
// each placeholder line measures 2 cells so table alignment holds.
package main

import (
	"fmt"
	"os"

	"github.com/abhirupdas/herdr-archive/internal/brands"
	"github.com/abhirupdas/herdr-archive/internal/kitty"
	"github.com/charmbracelet/x/ansi"
)

func main() {
	fmt.Println("kitty capable:", kitty.Capable())
	if !kitty.Capable() {
		os.Exit(1)
	}
	icons := kitty.Setup(os.Stdout, brands.Logo, brands.Kinds)
	defer fmt.Fprint(os.Stdout, kitty.DeleteAll())
	for _, k := range brands.Kinds {
		id, ok := icons.Icon(k, 1)
		if !ok {
			fmt.Printf("%-14s (no image)\n", k)
			continue
		}
		ph := kitty.Placeholder(id, 2, 1)[0]
		w := ansi.StringWidth(ph)
		mark := "ok"
		if w != 2 {
			mark = fmt.Sprintf("BAD WIDTH %d", w)
		}
		fmt.Println(ph + " " + k + "  [" + mark + "]")
	}
}
