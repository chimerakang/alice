package app

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Per-item acceptance (issue #224).
//
// A ship task dispatched from a GitHub issue keeps the issue's acceptance
// criteria as a versioned snapshot with fixed IDs. The agent reports per ID
// what evidence it submitted, what is missing, or what waits on someone
// else; the reviewer decides per ID. Everything is an append-only log, and
// the state shown to the reviewer is folded from it, so a round, a decision
// or a note is never overwritten.

// Log kinds.
const (
	acceptKindCriteria  = "criteria"  // a version of the criteria took effect
	acceptKindProposal  = "proposal"  // the issue changed; awaiting the reviewer
	acceptKindDismissed = "dismissed" // the reviewer kept the current criteria
	acceptKindReport    = "report"    // one run's report (agent) or a manual entry
	acceptKindDecision  = "decision"  // the reviewer's call on one item
	acceptKindRequest   = "request"   // evidence was asked for, in the same task
)

// What an agent (or a reviewer filling in) can report for an item.
const (
	acceptReportEvidence = "evidence_submitted"
	acceptReportNeeds    = "needs_evidence"
	acceptReportExternal = "awaiting_external"
	acceptReportNone     = "not_reported"
)

// What a reviewer can decide for an item.
const (
	acceptDecisionPending  = "pending"
	acceptDecisionApproved = "approved"
	acceptDecisionNeeds    = "needs_evidence"
	acceptDecisionExternal = "awaiting_external"
)

// Evidence kinds.
const (
	acceptEvidenceCommit = "commit"
	acceptEvidencePR     = "pr"
	acceptEvidenceReport = "report"
	acceptEvidenceTest   = "test"
	acceptEvidenceFile   = "file"
)

// Why an approval was withdrawn.
const (
	acceptReopenCriteria = "criteria_changed"
	acceptReopenEvidence = "evidence_changed"
)

// Gate failure codes.
const (
	acceptGateItemsOpen       = "items_not_approved"
	acceptGateDeliveryPending = "delivery_incomplete"
)

type acceptCriterion struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// acceptEvidence is one piece of evidence. Ref is the commit, PR URL or
// number, report path or URL, or test command; Paths are the files it
// concerns; Note is free text (a test log excerpt, say).
type acceptEvidence struct {
	Type  string   `json:"type"`
	Ref   string   `json:"ref,omitempty"`
	Paths []string `json:"paths,omitempty"`
	Note  string   `json:"note,omitempty"`
}

type acceptBlocker struct {
	Missing string `json:"missing"`
	Owner   string `json:"owner"`
}

type acceptReportItem struct {
	ID       string           `json:"id"`
	Status   string           `json:"status"`
	Evidence []acceptEvidence `json:"evidence,omitempty"`
	Blocker  *acceptBlocker   `json:"blocker,omitempty"`
	Note     string           `json:"note,omitempty"`
}

// acceptReport is the payload of a report log entry. Rejected reports keep
// their errors and no items: nothing in them took effect.
type acceptReport struct {
	Run    int                `json:"run"`
	Source string             `json:"source"` // agent | reviewer
	Items  []acceptReportItem `json:"items,omitempty"`
	Errors []string           `json:"errors,omitempty"`
}

type acceptCriteriaEntry struct {
	Version int               `json:"version"`
	Source  string            `json:"source"` // dispatch | issue | manual
	Items   []acceptCriterion `json:"items"`
}

type acceptDiffChange struct {
	ID   string `json:"id"`
	Old  string `json:"old,omitempty"`
	Text string `json:"text,omitempty"`
}

// acceptDiff is what an issue edit would change.
type acceptDiff struct {
	Added   []acceptCriterion  `json:"added,omitempty"`
	Removed []acceptCriterion  `json:"removed,omitempty"`
	Changed []acceptDiffChange `json:"changed,omitempty"`
}

func (d acceptDiff) empty() bool { return len(d.Added)+len(d.Removed)+len(d.Changed) == 0 }

type acceptProposal struct {
	Version int               `json:"version"`
	Items   []acceptCriterion `json:"items"`
	Diff    acceptDiff        `json:"diff"`
}

type acceptDecision struct {
	ItemID   string `json:"item_id"`
	Decision string `json:"decision"`
	Note     string `json:"note,omitempty"`
}

type acceptRequest struct {
	Items []string `json:"items"`
	Note  string   `json:"note,omitempty"`
}

// acceptLogEntry is one stored record.
type acceptLogEntry struct {
	ID        int64
	TaskID    string
	Kind      string
	RunNo     int
	Actor     string
	Payload   json.RawMessage
	CreatedAt time.Time
}

// acceptHistory is one line of an item's history.
type acceptHistory struct {
	At     time.Time
	Actor  string
	Kind   string // report | decision | reopened
	Status string
	Note   string
	Run    int
	Round  int
}

// acceptItem is one acceptance item as the reviewer sees it.
type acceptItem struct {
	ID      string
	Text    string
	Version int // criteria version in which this text took effect

	Report   string // evidence_submitted | needs_evidence | awaiting_external | not_reported
	Evidence []acceptEvidence
	Blocker  *acceptBlocker
	Round    int // 0 when never reported

	Decision     string
	Note         string
	DecidedBy    string
	Reopened     string // why a former approval was withdrawn, or ""
	Removed      bool
	History      []acceptHistory
	EvidenceKeys string
}

type acceptState struct {
	// Structured is false for a task with no criteria on record.
	Structured bool
	Version    int
	Items      []acceptItem
	Proposal   *acceptProposal
	// Rejected are the reports that took no effect, newest last.
	Rejected     []acceptReport
	Rounds       int
	ReportedRuns map[int]bool
	// Requested is the item set of the last evidence request not yet
	// answered by a later report.
	Requested []string
}

func (s acceptState) item(id string) (*acceptItem, bool) {
	for i := range s.Items {
		if s.Items[i].ID == id && !s.Items[i].Removed {
			return &s.Items[i], true
		}
	}
	return nil, false
}

// active returns the items that count: those still in the criteria.
func (s acceptState) active() []acceptItem {
	var out []acceptItem
	for _, it := range s.Items {
		if !it.Removed {
			out = append(out, it)
		}
	}
	return out
}

// Counts reports how many items are approved, and how many there are.
func (s acceptState) Counts() (approved, total int) {
	for _, it := range s.active() {
		total++
		if it.Decision == acceptDecisionApproved {
			approved++
		}
	}
	return
}

// ExternalWait reports whether any item waits on someone outside the fleet.
func (s acceptState) ExternalWait() bool {
	for _, it := range s.active() {
		if it.Decision == acceptDecisionExternal {
			return true
		}
	}
	return false
}

func evidenceKey(ev []acceptEvidence) string {
	b, _ := json.Marshal(ev)
	return string(b)
}

// foldAcceptance derives the state from the log, oldest entry first.
func foldAcceptance(entries []acceptLogEntry) acceptState {
	st := acceptState{ReportedRuns: map[int]bool{}}
	byID := map[string]*acceptItem{}
	order := []string{}

	reopen := func(it *acceptItem, at time.Time, actor, why string) {
		if it.Decision != acceptDecisionApproved {
			return
		}
		it.Decision, it.Reopened = acceptDecisionPending, why
		it.History = append(it.History, acceptHistory{At: at, Actor: actor, Kind: "reopened", Status: why})
	}

	for _, e := range entries {
		switch e.Kind {
		case acceptKindCriteria:
			var c acceptCriteriaEntry
			if json.Unmarshal(e.Payload, &c) != nil {
				continue
			}
			st.Structured, st.Version, st.Proposal = true, c.Version, nil
			keep := map[string]bool{}
			for _, cr := range c.Items {
				keep[cr.ID] = true
				it, ok := byID[cr.ID]
				if !ok {
					it = &acceptItem{ID: cr.ID, Text: cr.Text, Version: c.Version, Report: acceptReportNone, Decision: acceptDecisionPending}
					byID[cr.ID] = it
					order = append(order, cr.ID)
					continue
				}
				if it.Removed {
					it.Removed = false
				}
				if it.Text != cr.Text {
					it.Text, it.Version = cr.Text, c.Version
					reopen(it, e.CreatedAt, e.Actor, acceptReopenCriteria)
				}
			}
			for id, it := range byID {
				if !keep[id] {
					it.Removed = true
				}
			}
		case acceptKindProposal:
			var p acceptProposal
			if json.Unmarshal(e.Payload, &p) == nil {
				st.Proposal = &p
			}
		case acceptKindDismissed:
			st.Proposal = nil
		case acceptKindReport:
			var r acceptReport
			if json.Unmarshal(e.Payload, &r) != nil {
				continue
			}
			if r.Source == "agent" {
				st.ReportedRuns[r.Run] = true
			}
			if len(r.Errors) > 0 {
				st.Rejected = append(st.Rejected, r)
				continue
			}
			st.Rounds++
			for _, ri := range r.Items {
				it, ok := byID[ri.ID]
				if !ok || it.Removed {
					continue
				}
				key := evidenceKey(ri.Evidence)
				if key != it.EvidenceKeys || ri.Status != it.Report {
					reopen(it, e.CreatedAt, e.Actor, acceptReopenEvidence)
				}
				it.Report, it.Evidence, it.Blocker, it.Round = ri.Status, ri.Evidence, ri.Blocker, st.Rounds
				it.EvidenceKeys = key
				// A new answer to a request for evidence or an outside
				// reply puts the item back in front of the reviewer.
				if (it.Decision == acceptDecisionNeeds || it.Decision == acceptDecisionExternal) && ri.Status == acceptReportEvidence {
					it.Decision = acceptDecisionPending
				}
				it.History = append(it.History, acceptHistory{
					At: e.CreatedAt, Actor: e.Actor, Kind: "report", Status: ri.Status, Note: ri.Note, Run: r.Run, Round: st.Rounds,
				})
			}
			st.Requested = nil
		case acceptKindDecision:
			var d acceptDecision
			if json.Unmarshal(e.Payload, &d) != nil {
				continue
			}
			it, ok := byID[d.ItemID]
			if !ok || it.Removed {
				continue
			}
			it.Decision, it.Note, it.DecidedBy, it.Reopened = d.Decision, d.Note, e.Actor, ""
			it.History = append(it.History, acceptHistory{At: e.CreatedAt, Actor: e.Actor, Kind: "decision", Status: d.Decision, Note: d.Note})
		case acceptKindRequest:
			var q acceptRequest
			if json.Unmarshal(e.Payload, &q) == nil {
				st.Requested = q.Items
			}
		}
	}
	for _, id := range order {
		st.Items = append(st.Items, *byID[id])
	}
	return st
}

// ── criteria ────────────────────────────────────────────────────────────────

func normalizeCriterionText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// reconcileCriteria matches a freshly read list of criteria against the ones
// on record, keeping each item's ID. An item whose text is unchanged keeps
// its ID. Of the rest, old and new are paired in order (an edited item keeps
// its ID); unpaired new items get a fresh ID and unpaired old ones are
// removed.
func reconcileCriteria(current, fresh []acceptCriterion) ([]acceptCriterion, acceptDiff) {
	var diff acceptDiff
	used := map[string]bool{}
	out := make([]acceptCriterion, len(fresh))
	matched := make([]bool, len(fresh))

	byText := map[string][]int{}
	for i, c := range current {
		byText[normalizeCriterionText(c.Text)] = append(byText[normalizeCriterionText(c.Text)], i)
	}
	oldUsed := make([]bool, len(current))
	for i, f := range fresh {
		key := normalizeCriterionText(f.Text)
		if idx := byText[key]; len(idx) > 0 {
			out[i] = acceptCriterion{ID: current[idx[0]].ID, Text: current[idx[0]].Text}
			oldUsed[idx[0]] = true
			used[out[i].ID] = true
			byText[key] = idx[1:]
			matched[i] = true
		}
	}
	var leftoverOld []int
	for i := range current {
		if !oldUsed[i] {
			leftoverOld = append(leftoverOld, i)
		}
	}
	next := 0
	for i, f := range fresh {
		if matched[i] {
			continue
		}
		if next < len(leftoverOld) {
			old := current[leftoverOld[next]]
			next++
			out[i] = acceptCriterion{ID: old.ID, Text: f.Text}
			used[old.ID] = true
			diff.Changed = append(diff.Changed, acceptDiffChange{ID: old.ID, Old: old.Text, Text: f.Text})
			continue
		}
		id := f.ID
		if id == "" || used[id] || criterionIDTaken(current, id) {
			id = freshCriterionID(current, used)
		}
		used[id] = true
		out[i] = acceptCriterion{ID: id, Text: f.Text}
		diff.Added = append(diff.Added, out[i])
	}
	for _, i := range leftoverOld[next:] {
		diff.Removed = append(diff.Removed, current[i])
	}
	return out, diff
}

func criterionIDTaken(list []acceptCriterion, id string) bool {
	for _, c := range list {
		if c.ID == id {
			return true
		}
	}
	return false
}

var criterionIDRe = regexp.MustCompile(`^item-(\d+)$`)

func freshCriterionID(current []acceptCriterion, used map[string]bool) string {
	max := 0
	consider := func(id string) {
		if m := criterionIDRe.FindStringSubmatch(id); m != nil {
			var n int
			fmt.Sscanf(m[1], "%d", &n)
			if n > max {
				max = n
			}
		}
	}
	for _, c := range current {
		consider(c.ID)
	}
	for id := range used {
		consider(id)
	}
	return fmt.Sprintf("item-%d", max+1)
}

// ── agent report ────────────────────────────────────────────────────────────

var acceptReportBlockRe = regexp.MustCompile("(?s)```acceptance-report[ \\t]*\\r?\\n(.*?)```")

// acceptReportInstructions is added to the task's goal, and to every request
// for evidence, so the agent knows how to report.
const acceptReportInstructions = "Report per acceptance item, by ID, in a fenced block in your final message:\n\n" +
	"```acceptance-report\n" +
	"{\"items\": [\n" +
	"  {\"id\": \"item-1\", \"status\": \"evidence_submitted\", \"evidence\": [\n" +
	"    {\"type\": \"commit\", \"ref\": \"<sha>\", \"paths\": [\"path/to/file\"]},\n" +
	"    {\"type\": \"pr\", \"ref\": \"<url or #number>\"},\n" +
	"    {\"type\": \"report\", \"ref\": \"<path or url>\"},\n" +
	"    {\"type\": \"test\", \"ref\": \"<command>\", \"note\": \"<result>\"}]},\n" +
	"  {\"id\": \"item-2\", \"status\": \"needs_evidence\", \"note\": \"<what is missing>\"},\n" +
	"  {\"id\": \"item-3\", \"status\": \"awaiting_external\", \"blocker\": {\"missing\": \"<what is missing>\", \"owner\": \"<who must act>\"}}\n" +
	"]}\n```\n\n" +
	"Status is evidence_submitted, needs_evidence or awaiting_external. Name each ID at most once and only IDs from the list; a report with an unknown or repeated ID is rejected whole. " +
	"An item you do not name counts as not reported. Work that changed no files is fine: cite the existing commit or PR, or attach the research report or test record as evidence; do not create an empty commit or PR."

// parseAcceptanceReport reads the last acceptance-report block of a final
// message. found is false when the message has none. Problems come back as
// errors, in which case the report must take no effect.
func parseAcceptanceReport(text string, known func(id string) bool) (items []acceptReportItem, errs []string, found bool) {
	blocks := acceptReportBlockRe.FindAllStringSubmatch(text, -1)
	if len(blocks) == 0 {
		return nil, nil, false
	}
	var body struct {
		Items []acceptReportItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(blocks[len(blocks)-1][1]), &body); err != nil {
		return nil, []string{"invalid_json"}, true
	}
	items, errs = validateReportItems(body.Items, known)
	return items, errs, true
}

// validateReportItems is the shared rule set for agent reports and manual
// entries. Any error means the whole report takes no effect.
func validateReportItems(in []acceptReportItem, known func(id string) bool) ([]acceptReportItem, []string) {
	var errs []string
	seen := map[string]bool{}
	out := make([]acceptReportItem, 0, len(in))
	if len(in) == 0 {
		return nil, []string{"empty_report"}
	}
	for _, it := range in {
		it.ID = strings.ToLower(strings.TrimSpace(it.ID))
		it.Note = strings.TrimSpace(it.Note)
		switch {
		case it.ID == "" || !known(it.ID):
			errs = append(errs, "unknown_id:"+it.ID)
			continue
		case seen[it.ID]:
			errs = append(errs, "duplicate_id:"+it.ID)
			continue
		}
		seen[it.ID] = true
		switch it.Status {
		case acceptReportEvidence:
			if len(it.Evidence) == 0 {
				errs = append(errs, "no_evidence:"+it.ID)
			}
			for i := range it.Evidence {
				ev := &it.Evidence[i]
				ev.Type, ev.Ref, ev.Note = strings.ToLower(strings.TrimSpace(ev.Type)), strings.TrimSpace(ev.Ref), strings.TrimSpace(ev.Note)
				switch ev.Type {
				case acceptEvidenceCommit, acceptEvidencePR, acceptEvidenceReport, acceptEvidenceTest, acceptEvidenceFile:
				default:
					errs = append(errs, "bad_evidence_type:"+it.ID)
					continue
				}
				if ev.Ref == "" && len(ev.Paths) == 0 && ev.Note == "" {
					errs = append(errs, "empty_evidence:"+it.ID)
				}
				if ev.Type == acceptEvidenceCommit && ev.Ref == "" {
					errs = append(errs, "commit_without_sha:"+it.ID)
				}
			}
			it.Blocker = nil
		case acceptReportNeeds:
			if it.Note == "" {
				errs = append(errs, "needs_evidence_without_note:"+it.ID)
			}
		case acceptReportExternal:
			if it.Blocker == nil || strings.TrimSpace(it.Blocker.Missing) == "" || strings.TrimSpace(it.Blocker.Owner) == "" {
				errs = append(errs, "blocker_incomplete:"+it.ID)
			}
		default:
			errs = append(errs, "bad_status:"+it.ID)
		}
		out = append(out, it)
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return nil, errs
	}
	return out, nil
}

// ── gate ────────────────────────────────────────────────────────────────────

// acceptGate says whether a task may be closed: every item on record is
// approved, and the delivery flow the project asks for is complete. A task
// without criteria on record is not gated (it predates them).
type acceptGate struct {
	OK      bool
	Code    string
	Open    []string // item IDs not approved
	Message string
}

func evaluateAcceptGate(st acceptState, deliveryDone bool) acceptGate {
	if !st.Structured {
		return acceptGate{OK: true}
	}
	var open []string
	for _, it := range st.active() {
		if it.Decision != acceptDecisionApproved {
			open = append(open, it.ID)
		}
	}
	switch {
	case len(open) > 0:
		return acceptGate{Code: acceptGateItemsOpen, Open: open, Message: "acceptance items not approved: " + strings.Join(open, ", ")}
	case !deliveryDone:
		return acceptGate{Code: acceptGateDeliveryPending, Message: "the project's delivery flow is not complete (no pull request yet)"}
	}
	return acceptGate{OK: true}
}
