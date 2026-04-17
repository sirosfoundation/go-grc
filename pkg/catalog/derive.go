package catalog

import "github.com/sirosfoundation/go-grc/pkg/audit"

// ControlStatus constants for derived control states.
const (
	ControlValidated  = "validated"   // all findings resolved with evidence
	ControlVerified   = "verified"    // all findings resolved, some lack evidence
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
		derived := deriveFromFindings(findings)
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

func deriveFromFindings(findings []*audit.FindingRef) string {
	allTerminal, allEvidence, anyActive := true, true, false
	for _, fref := range findings {
		f := fref.Finding
		if !f.IsTerminal() {
			allTerminal = false
		}
		if !f.HasEvidence() {
			allEvidence = false
		}
		if f.IsActive() {
			anyActive = true
		}
	}
	switch {
	case allTerminal && allEvidence:
		return ControlValidated
	case allTerminal:
		return ControlVerified
	case anyActive:
		return ControlInProgress
	default:
		return ControlToDo
	}
}
