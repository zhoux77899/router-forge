package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func (s *Store) CreateRun(ctx context.Context, snapshot domain.RunSnapshot) (domain.Run, error) {
	r := domain.Run{ID: domain.NewID("run"), State: "draft", CreatedAt: domain.Now(), Snapshot: snapshot, Tasks: []domain.Task{}, Summary: domain.RequestSummary{Costs: []domain.CurrencyTotal{}}}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return r, err
	}
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "INSERT INTO evaluation_runs(id,state,snapshot_json,created_at) VALUES(?,?,?,?)", r.ID, r.State, string(b), r.CreatedAt); err != nil {
		return r, err
	}
	for _, name := range snapshot.Spec.TaskNames {
		task := domain.Task{ID: domain.NewID("task"), RunID: r.ID, Name: name, State: "pending", Attempts: []domain.Attempt{}}
		a := domain.Attempt{ID: domain.NewID("attempt"), TaskID: task.ID, RunID: r.ID, State: "pending", ImageDigests: []string{}}
		a.JobName = "rf-" + a.ID
		ab, _ := json.Marshal(a)
		if _, err = tx.ExecContext(ctx, "INSERT INTO evaluation_tasks(id,run_id,name,state) VALUES(?,?,?,?)", task.ID, r.ID, task.Name, task.State); err != nil {
			return r, err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO evaluation_attempts(id,task_id,run_id,state,job_name,result_json) VALUES(?,?,?,?,?,?)", a.ID, a.TaskID, a.RunID, a.State, a.JobName, string(ab)); err != nil {
			return r, err
		}
		task.Attempts = append(task.Attempts, a)
		r.Tasks = append(r.Tasks, task)
	}
	if err = tx.Commit(); err != nil {
		return r, err
	}
	return r, nil
}

type runRow struct {
	ID         string  `db:"id"`
	State      string  `db:"state"`
	Snapshot   string  `db:"snapshot_json"`
	CreatedAt  string  `db:"created_at"`
	StartedAt  *string `db:"started_at"`
	FinishedAt *string `db:"finished_at"`
	Error      string  `db:"error"`
}

func (s *Store) Run(ctx context.Context, id string) (domain.Run, error) {
	var row runRow
	var r domain.Run
	err := s.DB.GetContext(ctx, &row, "SELECT id,state,snapshot_json,created_at,started_at,finished_at,error FROM evaluation_runs WHERE id=?", id)
	if err == sql.ErrNoRows {
		return r, domain.ErrNotFound
	}
	if err != nil {
		return r, err
	}
	r = domain.Run{ID: row.ID, State: row.State, CreatedAt: row.CreatedAt, StartedAt: row.StartedAt, FinishedAt: row.FinishedAt, Error: row.Error, Tasks: []domain.Task{}}
	if err = json.Unmarshal([]byte(row.Snapshot), &r.Snapshot); err != nil {
		return r, err
	}
	type taskRow struct {
		ID    string `db:"id"`
		Name  string `db:"name"`
		State string `db:"state"`
	}
	var tasks []taskRow
	if err = s.DB.SelectContext(ctx, &tasks, "SELECT id,name,state FROM evaluation_tasks WHERE run_id=? ORDER BY id", id); err != nil {
		return r, err
	}
	for _, row := range tasks {
		t := domain.Task{ID: row.ID, RunID: id, Name: row.Name, State: row.State, Attempts: []domain.Attempt{}}
		var attempts []string
		if err = s.DB.SelectContext(ctx, &attempts, "SELECT result_json FROM evaluation_attempts WHERE task_id=? ORDER BY id", row.ID); err != nil {
			return r, err
		}
		for _, raw := range attempts {
			var a domain.Attempt
			if err = json.Unmarshal([]byte(raw), &a); err != nil {
				return r, err
			}
			t.Attempts = append(t.Attempts, a)
		}
		r.Tasks = append(r.Tasks, t)
	}
	r.Summary, err = s.RequestSummary(ctx, id)
	return r, err
}

func (s *Store) Runs(ctx context.Context, limit, offset int) (domain.Page[domain.Run], error) {
	p := domain.Page[domain.Run]{Items: []domain.Run{}}
	if err := s.DB.GetContext(ctx, &p.Total, "SELECT count(*) FROM evaluation_runs"); err != nil {
		return p, err
	}
	var ids []string
	if err := s.DB.SelectContext(ctx, &ids, "SELECT id FROM evaluation_runs ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?", limit, offset); err != nil {
		return p, err
	}
	for _, id := range ids {
		r, err := s.Run(ctx, id)
		if err != nil {
			return p, err
		}
		p.Items = append(p.Items, r)
	}
	return p, nil
}

func (s *Store) TransitionRun(ctx context.Context, id, from, to, message string) error {
	now := domain.Now()
	var started, finished any
	if to == "preparing" || to == "running" {
		started = now
	}
	if to == "completed" || to == "failed" || to == "cancelled" || to == "interrupted" {
		finished = now
	}
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE evaluation_runs SET state=?,error=?,started_at=COALESCE(started_at,?),finished_at=COALESCE(finished_at,?) WHERE id=? AND state=?`, to, message, started, finished, id, from)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.ErrConflict
	}
	if finished != nil {
		var attempts []string
		if err = tx.SelectContext(ctx, &attempts, "SELECT result_json FROM evaluation_attempts WHERE run_id=? AND state IN ('pending','preparing','running','cancelling')", id); err != nil {
			return err
		}
		for _, raw := range attempts {
			var attempt domain.Attempt
			if err = json.Unmarshal([]byte(raw), &attempt); err != nil {
				return err
			}
			attempt.State = to
			attempt.FinishedAt = &now
			if message != "" {
				attempt.Error = message
			}
			var b []byte
			if b, err = json.Marshal(attempt); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE evaluation_attempts SET state=?,result_json=? WHERE id=? AND run_id=?", to, string(b), attempt.ID, id); err != nil {
				return err
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE evaluation_tasks SET state=? WHERE run_id=? AND state IN ('pending','preparing','running','cancelling')", to, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SaveAttempt(ctx context.Context, a domain.Attempt) error {
	b, err := json.Marshal(a)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE evaluation_attempts SET state=?,result_json=? WHERE id=? AND run_id=? AND task_id=?", a.State, string(b), a.ID, a.RunID, a.TaskID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return domain.ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, "UPDATE evaluation_tasks SET state=? WHERE id=? AND run_id=?", a.State, a.TaskID, a.RunID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) AddEvent(ctx context.Context, e domain.JobEvent) error {
	if e.Timestamp == "" {
		e.Timestamp = domain.Now()
	}
	if len(e.Message) > 4096 {
		e.Message = e.Message[:4096]
	}
	_, err := s.DB.ExecContext(ctx, "INSERT OR IGNORE INTO job_events(run_id,attempt_id,timestamp,kind,message) VALUES(?,?,?,?,?)", e.RunID, e.AttemptID, e.Timestamp, e.Kind, e.Message)
	return err
}
func (s *Store) Events(ctx context.Context, runID string, after int64) ([]domain.JobEvent, error) {
	type row struct {
		ID        int64  `db:"id"`
		RunID     string `db:"run_id"`
		AttemptID string `db:"attempt_id"`
		Timestamp string `db:"timestamp"`
		Kind      string `db:"kind"`
		Message   string `db:"message"`
	}
	var rows []row
	if err := s.DB.SelectContext(ctx, &rows, "SELECT id,run_id,COALESCE(attempt_id,'') AS attempt_id,timestamp,kind,message FROM job_events WHERE run_id=? AND id>? ORDER BY id LIMIT 500", runID, after); err != nil {
		return nil, err
	}
	out := make([]domain.JobEvent, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.JobEvent{ID: r.ID, RunID: r.RunID, AttemptID: r.AttemptID, Timestamp: r.Timestamp, Kind: r.Kind, Message: r.Message})
	}
	return out, nil
}

func (s *Store) RecoverInterrupted(ctx context.Context) error {
	var ids []string
	if err := s.DB.SelectContext(ctx, &ids, "SELECT id FROM evaluation_runs WHERE state IN ('preparing','running','cancelling')"); err != nil {
		return err
	}
	for _, id := range ids {
		r, err := s.Run(ctx, id)
		if err != nil {
			return err
		}
		if err = s.TransitionRun(ctx, id, r.State, "interrupted", "Backend stopped; execution outcome is unknown. No automatic retry."); err != nil {
			return err
		}
		for _, t := range r.Tasks {
			for _, a := range t.Attempts {
				if a.State == "preparing" || a.State == "running" || a.State == "pending" || a.State == "cancelling" {
					a.State = "interrupted"
					a.Error = "Backend connection was lost; no automatic retry"
					now := domain.Now()
					a.FinishedAt = &now
					if err = s.SaveAttempt(ctx, a); err != nil {
						return err
					}
				}
			}
		}
		if err = s.AddEvent(ctx, domain.JobEvent{RunID: id, Kind: "interrupted", Message: "Recovered after backend shutdown. Start a new run only after explicit approval."}); err != nil {
			return err
		}
	}
	var rows []string
	if err := s.DB.SelectContext(ctx, &rows, "SELECT record_json FROM llm_requests WHERE finished_at IS NULL"); err != nil {
		return err
	}
	for _, raw := range rows {
		var r domain.RequestRecord
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return err
		}
		now := domain.Now()
		r.FinishedAt = &now
		r.State = "interrupted"
		r.ErrorClass = "backend_interrupted"
		r.Usage.Completeness = "partial"
		if err := s.EndRequest(ctx, r); err != nil {
			return fmt.Errorf("finalize interrupted request: %w", err)
		}
	}
	return nil
}
