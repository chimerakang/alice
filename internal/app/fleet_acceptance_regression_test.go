package app

import (
	"encoding/json"
	"github.com/chimerakang/alice-core/fleet"
	"testing"
)

func TestAcceptanceRegressionWithdrawnEvidenceMustReopenApproval(t *testing.T) {
	for _, status := range []string{acceptReportNeeds, acceptReportExternal} {
		t.Run(status, func(t *testing.T) {
			var entries []acceptLogEntry
			add := func(kind string, p any) {
				raw, _ := json.Marshal(p)
				entries = append(entries, acceptLogEntry{Kind: kind, Payload: raw})
			}
			add(acceptKindCriteria, acceptCriteriaEntry{Version: 1, Items: []acceptCriterion{{ID: "item-1", Text: "grade"}}})
			add(acceptKindReport, acceptReport{Source: "agent", Run: 1, Items: []acceptReportItem{{ID: "item-1", Status: acceptReportEvidence, Evidence: []acceptEvidence{{Type: acceptEvidenceReport, Ref: "report.md"}}}}})
			add(acceptKindDecision, acceptDecision{ItemID: "item-1", Decision: acceptDecisionApproved})
			add(acceptKindReport, acceptReport{Source: "agent", Run: 2, Items: []acceptReportItem{{ID: "item-1", Status: status, Note: "missing grade", Blocker: &acceptBlocker{Missing: "grade", Owner: "reviewer"}}}})
			st := foldAcceptance(entries)
			if g := evaluateAcceptGate(st, true); g.OK {
				t.Fatalf("gate allowed close after %s replaced approved evidence; item=%+v", status, st.Items[0])
			}
		})
	}
}
func TestAcceptanceRegressionNoPRCannotOfferApproveEvenEvidenceOnly(t *testing.T) {
	st := acceptState{Structured: true, Items: []acceptItem{{ID: "item-1", Decision: acceptDecisionApproved}}}
	set := fleetTaskActions(fleetActionInput{Task: fleet.Task{Shape: fleet.ShapeShip, Status: fleet.StatusReviewReady, TrustMode: fleet.TrustDirectPR}, Accept: st, EvidenceOnly: true})
	if set.Can(fleetOpApprove) {
		t.Fatalf("direct-pr task without PR offered approve: %+v", set)
	}
}
