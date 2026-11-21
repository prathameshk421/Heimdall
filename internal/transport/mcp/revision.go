package mcp

import (
	"context"
	"fmt"
	"strings"
)

// maxRevisionArtifact bounds the promoted profile text a profiler reads: a profile is at most 64 KiB.
const maxRevisionArtifact = 64 << 10

// revisionInput is the revision input of a profiler task: the person's request and the promoted profile as it is now, so
// the profiler keeps what works and changes only what was asked (D184). It is empty for a task about a repository with
// no request and no promoted profile, so a first proposal reads as it did before.
func (s *Server) revisionInput(ctx context.Context, a Assignment) (string, error) {
	var b strings.Builder
	if r := a.Revision; r != nil {
		b.WriteString("Requested by " + string(r.By) + ": " + r.Note + "\n\n")
	}
	if s.deps.Profiles == nil {
		return b.String(), nil
	}
	artifact, err := s.deps.Profiles.PromotedArtifact(ctx, a.Task.Change.Repo)
	if err != nil {
		return "", fmt.Errorf("read the promoted sandbox profile: %w", err)
	}
	artifact = strings.TrimSpace(artifact)
	if artifact == "" {
		return b.String(), nil
	}
	if len(artifact) > maxRevisionArtifact {
		artifact = artifact[:maxRevisionArtifact]
	}
	b.WriteString("The promoted profile of this repository, as it is now:\n\n" + artifact)
	return b.String(), nil
}
