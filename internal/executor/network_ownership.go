package executor

import (
	"context"
	"net"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Relay access is revalidated using actual immutable Docker IDs and current
// endpoint IPs for every request. No subnet, private-LAN, or NAT fallback exists.
func (t *dockerTracker) authorizeNetworkPeer(ctx context.Context, peer net.IP, stage, directory string) bool {
	if peer == nil || peer.IsLoopback() || peer.IsUnspecified() || stage != "agent" && stage != "verifier" {
		return false
	}
	if t.attemptDir == "" || !pathWithin(t.attemptDir, directory) || filepath.Clean(directory) != directory {
		return false
	}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if t.collect(bounded) != nil {
		return false
	}
	t.mu.Lock()
	ids := make([]string, 0, len(t.containers))
	for id := range t.containers {
		ids = append(ids, id)
	}
	t.mu.Unlock()
	matched := false
	for _, id := range ids {
		container, err := t.inspect(bounded, id)
		if err != nil || !container.Running {
			continue
		}
		owned := OwnsContainer(container, t.owner, t.spec.RunID, t.spec.AttemptID)
		if !owned {
			if stage != "verifier" || !t.projectOwned(container) {
				continue
			}
			project := container.Labels["com.docker.compose.project"]
			if !nativeVerifierProject(project, t.trialPrefix) {
				continue
			}
		}
		mounted := false
		for _, mount := range container.Mounts {
			if filepath.Clean(mount.Source) == directory && mount.Destination == path.Join(containerNetworkRoot, stage) && !mount.RW {
				mounted = true
			}
		}
		if !mounted {
			continue
		}
		for _, endpoint := range container.Networks {
			if peer.Equal(net.ParseIP(endpoint.IPAddress)) || peer.Equal(net.ParseIP(endpoint.GlobalIPv6Address)) {
				// Ambiguous shared/NAT source addresses never grant access.
				if matched {
					return false
				}
				matched = true
			}
		}
	}
	return matched
}

func nativeVerifierProject(project, prefix string) bool {
	// Project membership itself is recorded from an actual private trial folder;
	// this additionally prevents an unlabeled main/environment project from being
	// mistaken for a separate verifier when authorizing its credential relay.
	return prefix != "" && strings.HasPrefix(project, strings.ToLower(prefix)+"__") && strings.Contains(project, "__verifier__")
}
