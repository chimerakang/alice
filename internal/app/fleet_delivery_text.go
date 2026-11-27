package app

import (
	"strconv"
	"strings"
)

// renderFleetDelivery words a delivery summary for a Telegram message. The
// same summary feeds the web page, so the two say the same thing.
func renderFleetDelivery(loc func(string, map[string]string) string, d fleetDelivery, detailsURL string) string {
	var lines []string

	if d.DiffKnown {
		lines = append(lines, loc("fleet_result_changes", map[string]string{
			"{files}": strconv.Itoa(d.Files + d.Untracked), "{ins}": strconv.Itoa(d.Insertions), "{del}": strconv.Itoa(d.Deletions),
		}))
	}

	passed, total := 0, len(d.Checks)
	for _, c := range d.Checks {
		if c.Passed {
			passed++
		}
	}
	counts := map[string]string{"{passed}": strconv.Itoa(passed), "{total}": strconv.Itoa(total), "{run}": strconv.Itoa(d.ChecksRun)}
	switch {
	case d.Verification == deliveryVerifyPassed || d.Verification == deliveryVerifyFailed:
		lines = append(lines, loc("fleet_dv_verify_"+d.Verification, counts))
	default:
		lines = append(lines, loc("fleet_dv_verify_"+d.Verification, nil))
	}
	if d.ChecksPrevious && total > 0 {
		lines = append(lines, loc("fleet_dv_verify_previous", counts))
	}
	for _, c := range d.Checks {
		mark := "✅"
		if !c.Passed {
			mark = "❌"
		}
		lines = append(lines, "  "+mark+" "+fleetClip(firstNonEmpty(c.Command, c.Name), 80))
	}

	if d.StopReason != "" {
		lines = append(lines, loc("fleet_dv_stop", map[string]string{"{reason}": fleetClip(firstLineOf(d.StopReason), 300)}))
	}
	if d.Note != "" {
		lines = append(lines, loc("fleet_dv_note", map[string]string{"{note}": fleetClip(d.Note, 700)}))
	}
	section := func(header string, items []fleetDeliveryItem) {
		if len(items) == 0 {
			return
		}
		lines = append(lines, loc(header, nil))
		for _, it := range items {
			lines = append(lines, "• "+loc("fleet_dv_item_"+it.Code, map[string]string{"{detail}": fleetClip(firstLineOf(it.Detail), 120)}))
		}
	}
	section("fleet_dv_completed", d.Completed)
	section("fleet_dv_pending", d.Pending)
	section("fleet_dv_limits", d.Limits)

	if d.PRURL != "" {
		lines = append(lines, "🔗 "+d.PRURL)
	}
	if detailsURL != "" {
		lines = append(lines, loc("fleet_dv_details", map[string]string{"{url}": detailsURL}))
	}
	if d.Next != "" {
		lines = append(lines, loc("fleet_dv_next_"+d.Next, nil))
	}
	return strings.Join(lines, "\n")
}
