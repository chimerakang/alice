package app

import (
	"github.com/chimerakang/alice-core/hermes"
	"github.com/chimerakang/alice-core/issueops"
)

// issueops works on orchestrator-neutral work items. These helpers map Hermes
// sub-tasks onto that shape at the package boundary.

func issueOpsWorkItems(tasks []hermes.SubTask) []issueops.WorkItem {
	items := make([]issueops.WorkItem, 0, len(tasks))
	for _, st := range tasks {
		items = append(items, issueops.WorkItem{
			ID:               st.ID,
			Description:      st.Description,
			Done:             st.Status == hermes.SubTaskDone,
			ChecklistItemIDs: st.ChecklistItemIDs,
		})
	}
	return items
}
