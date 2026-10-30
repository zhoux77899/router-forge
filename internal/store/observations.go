package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/zhoux77899/router-forge/internal/domain"
)

func (s *Store) RecordExecutionObservation(ctx context.Context, runID, taskID, attemptID string, observation domain.ExecutionObservation) error {
	if err := observation.Validate(); err != nil {
		return err
	}
	tx, err := s.DB.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var row struct {
		Snapshot     string `db:"snapshot_json"`
		RunState     string `db:"run_state"`
		Attempt      string `db:"result_json"`
		AttemptState string `db:"attempt_state"`
		TaskName     string `db:"task_name"`
	}
	err = tx.GetContext(ctx, &row, `SELECT r.snapshot_json,r.state AS run_state,a.result_json,a.state AS attempt_state,t.name AS task_name FROM evaluation_runs r JOIN evaluation_tasks t ON t.run_id=r.id JOIN evaluation_attempts a ON a.run_id=r.id AND a.task_id=t.id WHERE r.id=? AND t.id=? AND a.id=?`, runID, taskID, attemptID)
	if err == sql.ErrNoRows {
		return domain.ErrNotFound
	}
	if err != nil {
		return err
	}
	if row.RunState != "running" && row.RunState != "preparing" || row.AttemptState != "running" && row.AttemptState != "preparing" {
		return domain.ErrConflict
	}
	var snapshot domain.RunSnapshot
	if err := json.Unmarshal([]byte(row.Snapshot), &snapshot); err != nil {
		return err
	}
	if len(snapshot.Spec.TaskNames) != 1 || snapshot.Spec.TaskNames[0] != row.TaskName {
		return errors.New("observation task identity is inconsistent")
	}
	if err := snapshot.ApplyObservation(observation); err != nil {
		return err
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE evaluation_runs SET snapshot_json=? WHERE id=? AND snapshot_json=? AND state IN ('preparing','running')`, string(b), runID, row.Snapshot)
	if err != nil {
		return err
	}
	if count, err := result.RowsAffected(); err != nil || count != 1 {
		return domain.ErrConflict
	}
	if observation.Kind == "agent" {
		var attempt domain.Attempt
		if err := json.Unmarshal([]byte(row.Attempt), &attempt); err != nil {
			return err
		}
		attempt.AgentVersion = observation.AgentVersion
		b, err := json.Marshal(attempt)
		if err != nil {
			return err
		}
		result, err = tx.ExecContext(ctx, `UPDATE evaluation_attempts SET result_json=? WHERE id=? AND run_id=? AND task_id=? AND result_json=? AND state IN ('preparing','running')`, string(b), attemptID, runID, taskID, row.Attempt)
		if err != nil {
			return err
		}
		if count, err := result.RowsAffected(); err != nil || count != 1 {
			return domain.ErrConflict
		}
	}
	return tx.Commit()
}
