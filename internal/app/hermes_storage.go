package app

import (
	"database/sql"
	"log"

	"claude-tg-agent/internal/app/hermes"
)

// buildHermesTaskStore creates a TaskStateStore backed by the app's SQLite database.
// Falls back to NoopTaskStore if the database is unavailable.
func buildHermesTaskStore() hermes.TaskStateStore {
	if globalStorage == nil {
		return &hermes.NoopTaskStore{}
	}
	ss, ok := globalStorage.(*SQLiteStorage)
	if !ok {
		return &hermes.NoopTaskStore{}
	}
	db, ok := ss.GetDB().(*sql.DB)
	if !ok {
		return &hermes.NoopTaskStore{}
	}
	store, err := hermes.NewSQLiteTaskStore(db)
	if err != nil {
		log.Printf("[hermes] failed to create task store: %v", err)
		return &hermes.NoopTaskStore{}
	}
	return store
}
