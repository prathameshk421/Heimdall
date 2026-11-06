package rootlessrunner

import (
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/adapters/profileyaml"
	"github.com/prathameshk421/flip_the_switch_hackathon/internal/ports"
)

// RepoProfile is the reviewed, repository-owned sandbox contract. The artifact lives in the
// profileyaml adapter so the server's propose path and the runner's trial and promotion paths share
// one validation authority.
type RepoProfile = profileyaml.RepoProfile

// Spec converts an artifact into the port-level contract.
func Spec(profile RepoProfile) ports.ProfileSpec { return profileyaml.Spec(profile) }

func validProfileName(name string) bool { return profileyaml.ProfileName.MatchString(name) }

// ParseApprovedProfile validates the syntax and policy bounds of a repository profile artifact.
func ParseApprovedProfile(data []byte, allowlistedImages []string) (RepoProfile, error) {
	return profileyaml.ParseApprovedProfile(data, allowlistedImages)
}

// LoadApprovedProfiles loads only regular YAML files from the runner's durable promoted directory.
func LoadApprovedProfiles(dir string, allowlistedImages []string) (map[string]RepoProfile, error) {
	return profileyaml.LoadApprovedProfiles(dir, allowlistedImages)
}

// ErrImageNotAllowed reports a profile absent from the pinned image policy.
var ErrImageNotAllowed = profileyaml.ErrImageNotAllowed
