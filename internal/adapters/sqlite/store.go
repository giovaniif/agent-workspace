package sqlite

import (
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/giovaniif/agent-workspace/internal/app"
	"github.com/giovaniif/agent-workspace/internal/domain"

	_ "modernc.org/sqlite"
)

const FlushInterval = 50 * time.Millisecond

//go:embed migrations/*.sql
var migrationFS embed.FS

var _ app.Store = (*Store)(nil)

type table string

const (
	tableWorkspaces table = "workspaces"
	tableTasks      table = "tasks"
	tableWorktrees  table = "worktrees"
	tableSessions   table = "sessions"
	tableViewed     table = "viewed"
	tableDrafts     table = "review_drafts"
	tableDevices    table = "devices"
	tableProjects   table = "projects"
)

var keyColumn = map[table]string{
	tableWorkspaces: "root",
	tableTasks:      "id",
	tableWorktrees:  "id",
	tableSessions:   "id",
	tableViewed:     "key",
	tableDrafts:     "id",
	tableDevices:    "id",
	tableProjects:   "root",
}

type Store struct {
	db *sql.DB

	mu      sync.Mutex
	pending map[table]map[string][]byte
	events  []domain.SessionEvent
	err     error

	wake    chan struct{}
	flushes chan chan error
	done    chan struct{}
	closed  sync.Once
}

func DefaultPath() (string, error) {
	if home := os.Getenv("AGENTWS_HOME"); home != "" {
		return filepath.Join(home, "state.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".agentws", "state.db"), nil
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{
		db:      db,
		pending: map[table]map[string][]byte{},
		wake:    make(chan struct{}, 1),
		flushes: make(chan chan error),
		done:    make(chan struct{}),
	}
	go s.writer()
	return s, nil
}

type migration struct {
	version int
	sql     string
}

func migrations() ([]migration, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}
	var out []migration
	for _, e := range entries {
		prefix, _, _ := strings.Cut(e.Name(), "_")
		v, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		body, err := migrationFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return nil, err
		}
		out = append(out, migration{v, string(body)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

func LatestVersion() int {
	ms, err := migrations()
	if err != nil || len(ms) == 0 {
		return 0
	}
	return ms[len(ms)-1].version
}

func migrate(db *sql.DB) error {
	ms, err := migrations()
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS schema_version(version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var current int
	err = tx.QueryRow(`SELECT version FROM schema_version`).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.Exec(`INSERT INTO schema_version VALUES (0)`)
	}
	if err != nil {
		return err
	}
	for _, m := range ms {
		if m.version <= current {
			continue
		}
		if _, err := tx.Exec(m.sql); err != nil {
			return fmt.Errorf("migration %d: %w", m.version, err)
		}
		if _, err := tx.Exec(`UPDATE schema_version SET version = ?`, m.version); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Version() (int, error) {
	var v int
	err := s.db.QueryRow(`SELECT version FROM schema_version`).Scan(&v)
	return v, err
}

func (s *Store) PutWorkspace(w domain.Workspace) { s.put(tableWorkspaces, w.Root, w) }
func (s *Store) PutTask(t domain.Task)           { s.put(tableTasks, t.ID, t) }
func (s *Store) PutWorktree(w domain.Worktree)   { s.put(tableWorktrees, w.ID, w) }
func (s *Store) PutSession(x domain.Session)     { s.put(tableSessions, x.ID, x) }

func (s *Store) PutEvent(ev domain.SessionEvent) {
	s.mu.Lock()
	s.events = append(s.events, ev)
	s.mu.Unlock()
	s.wakeWriter()
}

func (s *Store) DeleteWorkspace(root string) { s.enqueue(tableWorkspaces, root, nil) }

func (s *Store) DeleteWorktree(id string) { s.enqueue(tableWorktrees, id, nil) }

func (s *Store) DeleteSession(id string) {
	s.mu.Lock()
	kept := s.events[:0]
	for _, ev := range s.events {
		if ev.SessionID != id {
			kept = append(kept, ev)
		}
	}
	s.events = kept
	s.mu.Unlock()
	s.enqueue(tableSessions, id, nil)
}

func (s *Store) PutViewed(m domain.ViewedMark) { s.put(tableViewed, m.Key(), m) }

func (s *Store) DeleteViewed(key string) { s.enqueue(tableViewed, key, nil) }

func (s *Store) PutDraft(d domain.ReviewDraft) { s.put(tableDrafts, d.ID, d) }

func (s *Store) PutDevice(d domain.Device) { s.put(tableDevices, d.ID, d) }

func (s *Store) DeleteDevice(id string) { s.enqueue(tableDevices, id, nil) }

func (s *Store) PutProject(p domain.Project) { s.put(tableProjects, p.Root, p) }

func (s *Store) DeleteProject(root string) { s.enqueue(tableProjects, root, nil) }

func (s *Store) put(t table, key string, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		s.recordErr(err)
		return
	}
	s.enqueue(t, key, data)
}

func (s *Store) enqueue(t table, key string, data []byte) {
	s.mu.Lock()
	if s.pending[t] == nil {
		s.pending[t] = map[string][]byte{}
	}
	s.pending[t][key] = data
	s.mu.Unlock()
	s.wakeWriter()
}

func (s *Store) wakeWriter() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Store) writer() {
	var dirty bool
	timer := time.NewTimer(FlushInterval)
	defer timer.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-s.wake:
			if !dirty {
				dirty = true
				timer.Reset(FlushInterval)
			}
		case <-timer.C:
			if dirty {
				dirty = false
				s.recordErr(s.write())
			}
		case reply := <-s.flushes:
			dirty = false
			s.recordErr(s.write())
			reply <- s.takeErr()
		}
	}
}

func (s *Store) recordErr(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	s.err = errors.Join(s.err, err)
	s.mu.Unlock()
}

func (s *Store) takeErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.err
	s.err = nil
	return err
}

func (s *Store) write() error {
	s.mu.Lock()
	batch := s.pending
	s.pending = map[table]map[string][]byte{}
	events := s.events
	s.events = nil
	s.mu.Unlock()
	if len(batch) == 0 && len(events) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for t, rows := range batch {
		stmt, err := tx.Prepare(fmt.Sprintf(`INSERT OR REPLACE INTO %s(%s, data) VALUES (?, ?)`, t, keyColumn[t]))
		if err != nil {
			return err
		}
		del, err := tx.Prepare(fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, t, keyColumn[t]))
		if err != nil {
			_ = stmt.Close()
			return err
		}
		for key, data := range rows {
			if data == nil {
				_, err = del.Exec(key)
			} else {
				_, err = stmt.Exec(key, string(data))
			}
			if err != nil {
				_ = stmt.Close()
				_ = del.Close()
				return err
			}
		}
		_ = stmt.Close()
		_ = del.Close()
	}
	if err := writeEvents(tx, events); err != nil {
		return err
	}
	for id, data := range batch[tableSessions] {
		if data != nil {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM session_events WHERE session_id = ?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func writeEvents(tx *sql.Tx, events []domain.SessionEvent) error {
	touched := map[string]bool{}
	for _, ev := range events {
		data, err := json.Marshal(ev)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO session_events(session_id, data) VALUES (?, ?)`, ev.SessionID, string(data)); err != nil {
			return err
		}
		touched[ev.SessionID] = true
	}
	for id := range touched {
		_, err := tx.Exec(`DELETE FROM session_events WHERE session_id = ? AND id NOT IN
			(SELECT id FROM session_events WHERE session_id = ? ORDER BY id DESC LIMIT ?)`, id, id, app.EventsPerSession)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Flush() error {
	reply := make(chan error)
	select {
	case s.flushes <- reply:
		return <-reply
	case <-s.done:
		return errors.New("sqlite: store closed")
	}
}

func (s *Store) Close() error {
	err := errors.New("sqlite: store closed")
	s.closed.Do(func() {
		err = s.Flush()
		close(s.done)
		err = errors.Join(err, s.db.Close())
	})
	return err
}

func (s *Store) Load() (app.Snapshot, error) {
	var snap app.Snapshot
	var err error
	if snap.Workspaces, err = loadAll[domain.Workspace](s.db, tableWorkspaces); err != nil {
		return snap, err
	}
	if snap.Tasks, err = loadAll[domain.Task](s.db, tableTasks); err != nil {
		return snap, err
	}
	if snap.Worktrees, err = loadAll[domain.Worktree](s.db, tableWorktrees); err != nil {
		return snap, err
	}
	if snap.Sessions, err = loadAll[domain.Session](s.db, tableSessions); err != nil {
		return snap, err
	}
	if snap.Viewed, err = loadAll[domain.ViewedMark](s.db, tableViewed); err != nil {
		return snap, err
	}
	if snap.Drafts, err = loadAll[domain.ReviewDraft](s.db, tableDrafts); err != nil {
		return snap, err
	}
	if snap.Devices, err = loadAll[domain.Device](s.db, tableDevices); err != nil {
		return snap, err
	}
	if snap.Projects, err = loadAll[domain.Project](s.db, tableProjects); err != nil {
		return snap, err
	}
	snap.Events, err = loadEvents(s.db)
	return snap, err
}

func loadEvents(db *sql.DB) ([]domain.SessionEvent, error) {
	rows, err := db.Query(`SELECT data FROM session_events ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []domain.SessionEvent
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var ev domain.SessionEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return nil, fmt.Errorf("session_events row: %w", err)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func loadAll[T any](db *sql.DB, t table) ([]T, error) {
	rows, err := db.Query(fmt.Sprintf(`SELECT data FROM %s ORDER BY %s`, t, keyColumn[t]))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []T
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var v T
		if err := json.Unmarshal([]byte(data), &v); err != nil {
			return nil, fmt.Errorf("%s row: %w", t, err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
