package main

type Root struct {
	Workflows map[string]Workflow
}

func (r Root) WorkflowCount() int {
	return len(r.Workflows)
}

func (r Root) StepCount() int {
	n := 0
	for _, w := range r.Workflows {
		n += len(w.Steps)
	}
	return n
}

// Workflow captures the info needed to upgrade a workflow's steps
type Workflow struct {
	FilePath string
	Steps    []Step
}

// Step captures all of the information necessary to manage/replace a
// single "- uses:" entry in a workflow.
type Step struct {
	LineNumber int
	Action     Action
}

// Action represents an action and its version as found in the `uses`
// directive of a [Step]. Once resolved, a set of [UpgradeCandidates] will
// be available.
type Action struct {
	// The name of an action (e.g. actions/checkout)
	Name string
	// The current version ref in the file on disk (e.g. semver tag, branch
	// name, commit hash)
	Ref string
	// The current release, if any, resolved from the ref on disk
	Release Release
	// The "resolved" version candidates (if any)
	UpgradeCandidates UpgradeCandidates
}

// UpgradeCandidates capture possible upgrade versions.
type UpgradeCandidates struct {
	// Absolute latest release
	Latest Release
	// Latest release in the same major version, presumed to be compatible
	LatestCompatible Release
}

// Release contains the info necessary to compare one release to another.
type Release struct {
	Version    string
	CommitHash string
}

func (r Release) String() string {
	switch {
	case r.Version != "":
		return r.CommitHash + " @ " + r.Version
		// return fmt.Sprintf("version=%s commit=%s", r.Version, r.CommitHash)
	case r.CommitHash != "":
		return r.CommitHash
	default:
		return "<unknown version>"
	}
}

// Exists determines whether a [Release] has been populated.
func (r Release) Exists() bool {
	return r != (Release{})
}
