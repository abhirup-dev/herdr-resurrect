package main

import (
	"fmt"
	"github.com/abhirupdas/herdr-archive/internal/brands"
	"github.com/abhirupdas/herdr-archive/internal/kitty"
)

func main() {
	fmt.Println("kitty capable:", kitty.Capable())
	for _, k := range []string{"claude", "codex", "pi", "grok", "kimi"} {
		b, ok := brands.PNG(k)
		fmt.Printf("%-8s ok=%v bytes=%d\n", k, ok, len(b))
	}
}
