package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/chimerakang/alice-core/fleet"
)

// errFleetNoProject is returned when a topic has no project to dispatch to.
var errFleetNoProject = errors.New("topic is not bound to a fleet project")

// fleetCommandTimeout bounds the fleet work done for one Telegram command.
const fleetCommandTimeout = 30 * time.Second

// fleetActor names a Telegram user in the fleet's audit log.
func fleetActor(userID int64) fleet.Actor {
	return fleet.Actor{ID: strconv.FormatInt(userID, 10), Via: "telegram"}
}

// repoRoot returns the top level of the git repository containing dir, the
// form in which the fleet stores project paths.
func fleetRepoRoot(ctx context.Context, dir string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("%w: %s", fleet.ErrNotGitRepo, dir)
	}
	return strings.TrimSpace(string(out)), nil
}

// ProjectForDir returns the fleet project whose repository contains dir.
func (f *FleetService) ProjectForDir(ctx context.Context, dir string) (fleet.Project, error) {
	root, err := fleetRepoRoot(ctx, dir)
	if err != nil {
		return fleet.Project{}, err
	}
	return f.Store.GetProjectByRepoPath(ctx, root)
}

// EnsureProject returns the fleet project for dir, registering it with the
// default trust mode when it is not registered yet.
func (f *FleetService) EnsureProject(ctx context.Context, actor fleet.Actor, dir string) (project fleet.Project, created bool, err error) {
	project, err = f.ProjectForDir(ctx, dir)
	if err == nil {
		return project, false, nil
	}
	if !errors.Is(err, fleet.ErrProjectNotFound) {
		return fleet.Project{}, false, err
	}
	project, err = f.Commander.RegisterProject(ctx, actor, fleet.Project{RepoPath: dir, TrustMode: f.TrustMode})
	if errors.Is(err, fleet.ErrProjectExists) {
		// Registered by someone else in the meantime.
		project, err = f.ProjectForDir(ctx, dir)
		return project, false, err
	}
	return project, err == nil, err
}

// topicProjectDirLister is the part of storage the backfill reads.
type topicProjectDirLister interface {
	ListTopicProjectDirs() ([]string, error)
}

// BackfillTopicProjects registers every project path already bound to a
// topic. Paths that are registered, missing, or not git repositories are
// skipped, so running it again changes nothing.
func (f *FleetService) BackfillTopicProjects(ctx context.Context, storage topicProjectDirLister) (registered int, err error) {
	dirs, err := storage.ListTopicProjectDirs()
	if err != nil {
		return 0, err
	}
	actor := fleet.Actor{ID: "alice", Via: "startup"}
	for _, dir := range dirs {
		_, created, err := f.EnsureProject(ctx, actor, dir)
		switch {
		case err == nil && created:
			registered++
		case err != nil && !errors.Is(err, fleet.ErrNotGitRepo):
			log.Printf("[fleet] backfill %s: %v", dir, err)
		}
	}
	return registered, nil
}

// fleetTopicDir returns the project path bound to a topic with /project, or
// "" when there is none.
func (t *TelegramBot) fleetTopicDir(key chatKey) string {
	if globalStorage == nil {
		return ""
	}
	dir, err := globalStorage.GetTopicSetting(key.chatID, key.threadID)
	if err != nil {
		log.Printf("[fleet] read topic setting chat=%d thread=%d: %v", key.chatID, key.threadID, err)
		return ""
	}
	return strings.TrimSpace(dir)
}

// fleetTopicProject returns the fleet project of a topic. It returns
// errFleetNoProject when the topic is unbound or its path is not a fleet
// project.
func (t *TelegramBot) fleetTopicProject(ctx context.Context, key chatKey) (fleet.Project, error) {
	dir := t.fleetTopicDir(key)
	if dir == "" {
		return fleet.Project{}, errFleetNoProject
	}
	project, err := t.fleet.ProjectForDir(ctx, dir)
	if errors.Is(err, fleet.ErrProjectNotFound) || errors.Is(err, fleet.ErrNotGitRepo) {
		return fleet.Project{}, errFleetNoProject
	}
	return project, err
}

// fleetRequireProject is fleetTopicProject for command handlers: when the
// topic has no project it tells the user to run /project and reports false.
func (t *TelegramBot) fleetRequireProject(ctx context.Context, key chatKey) (fleet.Project, bool) {
	project, err := t.fleetTopicProject(ctx, key)
	if err == nil {
		return project, true
	}
	if errors.Is(err, errFleetNoProject) {
		t.send(key, t.getLocalizedMessage(key.chatID, "fleet_no_project", nil))
	} else {
		t.send(key, t.getLocalizedMessage(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
	}
	return fleet.Project{}, false
}

// handleFleetCommand handles the commands the fleet adds or takes over. It
// reports whether the command was handled; everything else falls through to
// the existing command handling. With the fleet disabled it handles nothing.
func (t *TelegramBot) handleFleetCommand(key chatKey, userID int64, text string) bool {
	if t.fleet == nil {
		return false
	}
	parts := strings.Fields(text)
	if len(parts) == 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), fleetCommandTimeout)
	defer cancel()

	switch strings.Split(parts[0], "@")[0] {
	case "/project":
		switch {
		case len(parts) == 1:
			return t.fleetShowProject(ctx, key)
		case parts[1] == "mode":
			t.fleetSetProjectMode(ctx, key, userID, parts[2:])
			return true
		}
	}
	return false
}

// fleetShowProject answers a bare /project with the topic's fleet settings.
// An unbound topic falls through to the usual usage message.
func (t *TelegramBot) fleetShowProject(ctx context.Context, key chatKey) bool {
	project, err := t.fleetTopicProject(ctx, key)
	if err != nil {
		return false
	}
	verify := t.getLocalizedMessage(key.chatID, "fleet_project_verify_none", nil)
	if len(project.VerifyCommands) > 0 {
		verify = strings.Join(project.VerifyCommands, "\n   ")
	}
	t.send(key, t.getLocalizedMessage(key.chatID, "fleet_project_info", map[string]string{
		"{name}":       project.Name,
		"{id}":         project.ID,
		"{path}":       project.RepoPath,
		"{branch}":     project.DefaultBranch,
		"{mode}":       string(project.TrustMode),
		"{verify}":     verify,
		"{concurrent}": strconv.Itoa(project.MaxConcurrent),
	}))
	return true
}

// fleetSetProjectMode handles /project mode <trust mode>.
func (t *TelegramBot) fleetSetProjectMode(ctx context.Context, key chatKey, userID int64, args []string) {
	if len(args) != 1 {
		t.send(key, t.getLocalizedMessage(key.chatID, "fleet_project_mode_usage", nil))
		return
	}
	mode, err := fleet.ParseTrustMode(args[0])
	if err != nil {
		t.send(key, t.getLocalizedMessage(key.chatID, "fleet_project_mode_usage", nil))
		return
	}
	project, ok := t.fleetRequireProject(ctx, key)
	if !ok {
		return
	}
	project, err = t.fleet.Commander.UpdateProject(ctx, fleetActor(userID), project.ID, "trust mode: "+string(mode),
		func(p *fleet.Project) error {
			p.TrustMode = mode
			return nil
		})
	if err != nil {
		t.send(key, t.getLocalizedMessage(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
		return
	}
	t.send(key, t.getLocalizedMessage(key.chatID, "fleet_project_mode_set", map[string]string{
		"{name}": project.Name,
		"{mode}": string(project.TrustMode),
	}))
}

// fleetAfterCommand runs after the existing command handling. When the
// command was a successful /project <path>, the path is registered as a
// fleet project if it is not one yet.
func (t *TelegramBot) fleetAfterCommand(key chatKey, userID int64, text string) {
	if t.fleet == nil {
		return
	}
	parts := strings.Fields(text)
	if len(parts) < 2 || strings.Split(parts[0], "@")[0] != "/project" || parts[1] == "mode" {
		return
	}
	// /project saved the path only if it was valid; read back what it saved.
	dir := t.fleetTopicDir(key)
	if dir == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), fleetCommandTimeout)
	defer cancel()
	project, created, err := t.fleet.EnsureProject(ctx, fleetActor(userID), dir)
	switch {
	case errors.Is(err, fleet.ErrNotGitRepo):
		t.send(key, t.getLocalizedMessage(key.chatID, "fleet_project_not_git", nil))
	case err != nil:
		t.send(key, t.getLocalizedMessage(key.chatID, "fleet_error", map[string]string{"{error}": err.Error()}))
	case created:
		t.send(key, t.getLocalizedMessage(key.chatID, "fleet_project_registered", map[string]string{
			"{name}": project.Name,
			"{mode}": string(project.TrustMode),
		}))
	}
}
