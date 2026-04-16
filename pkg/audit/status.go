package audit

// Finding status constants.
const (
	StatusOpen       = "open"
	StatusInProgress = "in_progress"
	StatusResolved   = "resolved"
	StatusAccepted   = "accepted" // risk accepted, will not fix
)

// IsTerminal reports whether the finding status represents a final state.
func (f *Finding) IsTerminal() bool {
	return f.Status == StatusResolved || f.Status == StatusAccepted
}

// IsActive reports whether the finding is actively being worked on.
func (f *Finding) IsActive() bool {
	return f.Status == StatusInProgress
}
