package catalog

import "github.com/sirosfoundation/go-grc/pkg/audit"

// ControlStatus constants for derived control states. Whether a verified
// control is backed by evidence is tracked separately in Control.Evidenced.
const (
	ControlVerified   = "verified"    // all findings resolved
	ControlInProgress = "in_progress" // at least one finding is being worked on
	ControlToDo       = "to_do"       // findings exist but none are resolved or active
)

// DeriveControlStatuses populates DerivedStatus on controls based on linked
// audit findings. It is safe to call multiple times (idempotent).
func DeriveControlStatuses(cat *Catalog, audits *audit.AuditSet) {
	for id, ctrl := range cat.Controls {
		findings := audits.FindingsByControl[id]
		if len(findings) == 0 {
			continue
		}
		derived, evidenced := deriveFromFindings(findings)
		ctrl.Evidenced = evidenced
		if derived != ctrl.Status {
			ctrl.DerivedStatus = derived
		}
	}
}

// EffectiveStatus returns DerivedStatus if set, otherwise Status.
func EffectiveStatus(ctrl *Control) string {
	if ctrl.DerivedStatus != "" {
		return ctrl.DerivedStatus
	}
	return ctrl.Status
}

func deriveFromFindings(findings []*audit.FindingRef) (string, bool) {
	return deriveFromFindingsForProfile(findings, "")
}

// deriveFromFindingsForProfile returns the derived status and whether every
// finding is resolved with evidence.
func deriveFromFindingsForProfile(findings []*audit.FindingRef, profile string) (string, bool) {
	allTerminal, allEvidence, anyActive := true, true, false
	for _, fref := range findings {
		f := fref.Finding
		if !f.IsTerminalForProfile(profile) {
			allTerminal = false
		}
		if len(f.EvidenceForProfile(profile)) == 0 {
			allEvidence = false
		}
		s := f.StatusForProfile(profile)
		if s == audit.StatusInProgress {
			anyActive = true
		}
	}
	switch {
	case allTerminal:
		return ControlVerified, allEvidence
	case anyActive:
		return ControlInProgress, false
	default:
		return ControlToDo, false
	}
}

// DeriveControlStatusesForProfile populates DerivedStatus using profile-specific
// finding statuses and evidence.
func DeriveControlStatusesForProfile(cat *Catalog, audits *audit.AuditSet, profile string) {
	for id, ctrl := range cat.Controls {
		findings := audits.FindingsByControl[id]
		if len(findings) == 0 {
			continue
		}
		derived, evidenced := deriveFromFindingsForProfile(findings, profile)
		ctrl.Evidenced = evidenced
		if derived != ctrl.Status {
			ctrl.DerivedStatus = derived
		} else {
			ctrl.DerivedStatus = ""
		}
	}
}
