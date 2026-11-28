package app

import (
	"context"
	"net/http"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	alicev1 "claude-tg-agent/gen/go/alice/v1"
)

func evidenceProto(in []acceptEvidence) []*alicev1.FleetAcceptanceEvidence {
	out := []*alicev1.FleetAcceptanceEvidence{}
	for _, e := range in {
		out = append(out, &alicev1.FleetAcceptanceEvidence{Type: e.Type, Ref: e.Ref, Paths: append([]string{}, e.Paths...), Note: e.Note})
	}
	return out
}

func criteriaProto(in []acceptCriterion) []*alicev1.FleetAcceptanceCriterion {
	out := []*alicev1.FleetAcceptanceCriterion{}
	for _, c := range in {
		out = append(out, &alicev1.FleetAcceptanceCriterion{Id: c.ID, Text: c.Text})
	}
	return out
}

// fleetAcceptanceProto adds the per-item acceptance to a delivery reply.
func fleetAcceptanceProto(out *alicev1.FleetDelivery, d fleetDelivery) {
	out.AcceptanceItems = []*alicev1.FleetAcceptanceItem{}
	st := d.Accept
	sum := &alicev1.FleetAcceptanceSummary{
		Structured: st.Structured, Version: int32(st.Version), Rounds: int32(st.Rounds),
		ExternalWait: st.ExternalWait(), GateOk: d.Gate.OK, GateCode: d.Gate.Code, GateOpen: append([]string{}, d.Gate.Open...),
		EvidenceOnly: d.EvidenceOnly, VerificationSkipped: d.VerificationSkipped,
		RejectedReports: []*alicev1.FleetRejectedReport{},
	}
	approved, total := st.Counts()
	sum.Approved, sum.Total = int32(approved), int32(total)
	for _, r := range st.Rejected {
		sum.RejectedReports = append(sum.RejectedReports, &alicev1.FleetRejectedReport{Run: int32(r.Run), Errors: r.Errors})
	}
	if p := st.Proposal; p != nil {
		prop := &alicev1.FleetCriteriaProposal{Version: int32(p.Version), Added: criteriaProto(p.Diff.Added), Removed: criteriaProto(p.Diff.Removed)}
		for _, c := range p.Diff.Changed {
			prop.Changed = append(prop.Changed, &alicev1.FleetCriteriaChange{Id: c.ID, Old: c.Old, Text: c.Text})
		}
		sum.Proposal = prop
	}
	out.Acceptance = sum
	for _, it := range st.active() {
		pi := &alicev1.FleetAcceptanceItem{
			Id: it.ID, Text: it.Text, Version: int32(it.Version), Report: it.Report, Evidence: evidenceProto(it.Evidence),
			Round: int32(it.Round), Decision: it.Decision, Note: it.Note, DecidedBy: it.DecidedBy, Reopened: it.Reopened,
			History: []*alicev1.FleetAcceptanceHistory{},
		}
		if it.Blocker != nil {
			pi.Blocker = &alicev1.FleetAcceptanceBlocker{Missing: it.Blocker.Missing, Owner: it.Blocker.Owner}
		}
		for _, h := range it.History {
			pi.History = append(pi.History, &alicev1.FleetAcceptanceHistory{
				At: timestamppb.New(h.At), Actor: h.Actor, Kind: h.Kind, Status: h.Status, Note: h.Note, Run: int32(h.Run), Round: int32(h.Round),
			})
		}
		out.AcceptanceItems = append(out.AcceptanceItems, pi)
	}
}

// fleetAcceptanceOp handles POST /api/fleet/tasks/{id}/acceptance/{op}:
//
//	decide            one item: approved | needs_evidence | awaiting_external | pending
//	entry             the reviewer fills in what was not reported
//	criteria          refresh | apply | dismiss the issue's acceptance items
//	request-evidence  send the items needing evidence back to the same task
func (wi *WebInterface) fleetAcceptanceOp(ctx context.Context, svc *FleetService, r *http.Request) (proto.Message, error) {
	id, op := r.PathValue("id"), r.PathValue("op")
	actor := wi.fleetWebActor()
	var err error
	switch op {
	case "decide":
		var req alicev1.FleetAcceptanceDecisionRequest
		if err = readFleetBody(r, &req); err != nil {
			return nil, err
		}
		_, err = svc.Decide(ctx, actor, id, req.ItemId, strings.TrimSpace(req.Decision), req.Note)
	case "entry":
		var req alicev1.FleetAcceptanceEntryRequest
		if err = readFleetBody(r, &req); err != nil {
			return nil, err
		}
		var criteria []acceptCriterion
		for _, c := range req.Criteria {
			criteria = append(criteria, acceptCriterion{ID: c.Id, Text: c.Text})
		}
		var items []acceptReportItem
		for _, it := range req.Items {
			ri := acceptReportItem{ID: it.Id, Status: it.Status, Note: it.Note}
			for _, e := range it.Evidence {
				ri.Evidence = append(ri.Evidence, acceptEvidence{Type: e.Type, Ref: e.Ref, Paths: e.Paths, Note: e.Note})
			}
			if it.Blocker != nil {
				ri.Blocker = &acceptBlocker{Missing: it.Blocker.Missing, Owner: it.Blocker.Owner}
			}
			items = append(items, ri)
		}
		_, err = svc.ManualEntry(ctx, actor, id, criteria, items)
	case "criteria":
		var req alicev1.FleetAcceptanceCriteriaRequest
		if err = readFleetBody(r, &req); err != nil {
			return nil, err
		}
		switch req.Action {
		case "refresh":
			_, err = svc.RefreshCriteria(ctx, actor, id)
		case "apply", "dismiss":
			_, err = svc.ResolveCriteria(ctx, actor, id, req.Action == "apply")
		default:
			return nil, fleetBadRequest("action must be refresh, apply or dismiss")
		}
	case "request-evidence":
		if _, err = svc.RequestEvidence(ctx, actor, id); err != nil {
			return nil, err
		}
	default:
		return nil, fleetBadRequest("unknown acceptance operation " + op)
	}
	if err != nil {
		return nil, err
	}
	if wi.bot != nil && wi.bot.fleetCards != nil {
		wi.bot.fleetCards.Refresh(ctx, id)
	}
	d, err := svc.Delivery(ctx, id)
	if err != nil {
		return nil, err
	}
	return fleetDeliveryProto(d), nil
}
