// stalecheck — prints the badge the TUI would compute for each session.
package main

import (
	"fmt"

	"github.com/abhirup-dev/herdr-resurrect/internal/capture"
	"github.com/abhirup-dev/herdr-resurrect/internal/manifest"
)

func main() {
	sessions, err := capture.Sessions()
	if err != nil {
		panic(err)
	}
	for _, s := range sessions {
		badge := "stopped"
		if s.Running {
			badge = "running"
		}
		path, err := manifest.Latest("", s.Name)
		if err != nil {
			fmt.Printf("%-12s %-8s (no snapshot)\n", s.Name, badge)
			continue
		}
		snap, _ := manifest.Load(path)
		if snap == nil || !s.Running {
			fmt.Printf("%-12s %-8s snapshot=%d agents\n", s.Name, badge, len(snap.AgentPanes()))
			continue
		}
		live, err := capture.LiveAgents(s.Name)
		if err != nil {
			fmt.Printf("%-12s %-8s (live query failed: %v)\n", s.Name, badge, err)
			continue
		}
		total := len(snap.AgentPanes())
		n := 0
		for _, p := range snap.AgentPanes() {
			for _, l := range live {
				if l == p.Name {
					n++
					break
				}
			}
		}
		if n < total {
			badge = "running/stale"
		}
		fmt.Printf("%-12s %-15s %d/%d agents live (live roster: %v)\n", s.Name, badge, n, total, live)
	}
}
