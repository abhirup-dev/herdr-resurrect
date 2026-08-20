package resume

import (
	"fmt"
	"time"

	"github.com/abhirup-dev/herdr-resurrect/internal/herdr"
)

const (
	agentDetectionAttempts = 45
	agentRenameAttempts    = 6
)

func waitForAgent(session, paneID string) error {
	for range agentDetectionAttempts {
		var probe struct {
			Agent struct {
				Agent string `json:"agent"`
			} `json:"agent"`
		}
		if err := herdr.RunInto(&probe, append(herdr.SessionScope(session), "agent", "get", paneID)...); err == nil && probe.Agent.Agent != "" {
			return nil
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("agent not detected in %s within %ds", paneID, agentDetectionAttempts)
}

func attachName(session, paneID, name string) error {
	if name == "" {
		return nil
	}
	var lastErr error
	for attempt := range agentRenameAttempts {
		candidate := name
		if attempt > 0 {
			candidate = fmt.Sprintf("%s-%d", name, attempt)
		}
		if _, err := herdr.Run(append(herdr.SessionScope(session), "agent", "rename", paneID, candidate)...); err == nil {
			if candidate != name {
				fmt.Printf("  !            %s: name taken, restored as %s\n", name, candidate)
			}
			return nil
		} else {
			lastErr = err
		}
	}
	return fmt.Errorf("rename: %w", lastErr)
}
