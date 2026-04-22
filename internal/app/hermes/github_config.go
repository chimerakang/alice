package hermes

// GithubCfg holds GitHub integration settings for a single Coordinator instance.
// It is populated from app.GithubIntegrationConfig in hermes_bridge.go.
type GithubCfg struct {
	Enabled         bool
	CommentOnStart  bool
	CommentOnDone   bool
	CommentOnFail   bool
	CommentOnBudget bool
	SyncChecklist   bool
	AutoCloseLabel  string
	FailureLabel    string
	TriggerTaskSync bool
}

// ShouldComment returns true when GitHub commenting is enabled for the given event.
func (g *GithubCfg) ShouldComment(event string) bool {
	if !g.Enabled {
		return false
	}
	switch event {
	case "start":
		return g.CommentOnStart
	case "complete":
		return g.CommentOnDone
	case "fail":
		return g.CommentOnFail
	case "budget_exceeded":
		return g.CommentOnBudget
	}
	return false
}
