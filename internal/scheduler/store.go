package scheduler

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yyZe0122/yunmengze-agent/pkg/schedulerapi"
	"github.com/yyZe0122/yunmengze-agent/pkg/sqliteerror"
)

const jobSelectColumns = `job_id,name,session_id,task_title,task_objective,execution_mode,skill_ids,model_ref,interval_seconds,next_run_at,timeout_seconds,max_retries,backoff_seconds,misfire_policy,idempotency_key,status,created_at,updated_at`

// MainModelRefFunc returns the daemon main selection ref (provider/model…) for H7 default pin.
type MainModelRefFunc func() string

type Store struct {
	db           *sql.DB
	mainModelRef MainModelRefFunc
}

// NewStore binds the scheduler directly to Core's SQLite database. The Core
// migration runner owns the schema; no second database or process is needed.
func NewStore(db *sql.DB) (*Store, error) {
	return NewStoreWithMainRef(db, nil)
}

// NewStoreWithMainRef is like NewStore but pins empty CreateRequest.ModelRef via mainRef.
func NewStoreWithMainRef(db *sql.DB, mainRef MainModelRefFunc) (*Store, error) {
	if db == nil {
		return nil, errors.New("scheduler database is required")
	}
	return &Store{db: db, mainModelRef: mainRef}, nil
}

func (s *Store) ClaimScheduledTasks(ctx context.Context, request schedulerapi.ClaimDueRequest) ([]schedulerapi.TaskRequest, error) {
	return s.ClaimDue(ctx, request)
}

func (s *Store) AcknowledgeScheduledTask(ctx context.Context, request schedulerapi.AcknowledgeRequest) error {
	_, err := s.Acknowledge(ctx, request)
	return err
}

func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

func (s *Store) Create(ctx context.Context, request schedulerapi.CreateRequest) (schedulerapi.Job, error) {
	request.Name = strings.TrimSpace(request.Name)
	request.SessionID = strings.TrimSpace(request.SessionID)
	request.TaskTitle = strings.TrimSpace(request.TaskTitle)
	request.TaskObjective = strings.TrimSpace(request.TaskObjective)
	request.IdempotencyKey = strings.TrimSpace(request.IdempotencyKey)
	request.MisfirePolicy = strings.ToLower(strings.TrimSpace(request.MisfirePolicy))
	request.ExecutionMode = normalizeExecutionMode(request.ExecutionMode)
	request.SkillIDs = normalizeSkillIDs(request.SkillIDs)
	request.ModelRef = strings.TrimSpace(request.ModelRef)
	if request.ModelRef == "" && s.mainModelRef != nil {
		request.ModelRef = strings.TrimSpace(s.mainModelRef())
	}
	if request.MisfirePolicy == "" {
		request.MisfirePolicy = schedulerapi.MisfireRunOnce
	}
	if request.TimeoutSeconds == 0 {
		request.TimeoutSeconds = 1800
	}
	if request.BackoffSeconds == 0 {
		request.BackoffSeconds = 30
	}
	if request.Name == "" || request.SessionID == "" || request.TaskTitle == "" || request.TaskObjective == "" || request.IdempotencyKey == "" {
		return schedulerapi.Job{}, errors.New("name, session_id, task_title, task_objective and idempotency_key are required")
	}
	if request.IntervalSeconds <= 0 || request.TimeoutSeconds <= 0 || request.MaxRetries < 0 || request.BackoffSeconds < 0 {
		return schedulerapi.Job{}, errors.New("invalid scheduler timing or retry settings")
	}
	if request.MisfirePolicy != schedulerapi.MisfireSkip && request.MisfirePolicy != schedulerapi.MisfireCatchUp && request.MisfirePolicy != schedulerapi.MisfireRunOnce {
		return schedulerapi.Job{}, errors.New("invalid misfire policy")
	}
	if request.ExecutionMode != schedulerapi.ExecutionModeAgent && request.ExecutionMode != schedulerapi.ExecutionModePlan {
		return schedulerapi.Job{}, errors.New("execution_mode must be agent or plan")
	}
	var sessionExists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE session_id=?`, request.SessionID).Scan(&sessionExists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return schedulerapi.Job{}, errors.New("session not found")
		}
		return schedulerapi.Job{}, err
	}
	nextRun, err := parseOptionalTime(request.NextRunAt, time.Now().UTC())
	if err != nil {
		return schedulerapi.Job{}, err
	}
	skillJSON, err := encodeSkillIDs(request.SkillIDs)
	if err != nil {
		return schedulerapi.Job{}, err
	}
	jobID, err := newID("job")
	if err != nil {
		return schedulerapi.Job{}, err
	}
	when := nowString()
	_, err = s.db.ExecContext(ctx, `INSERT INTO jobs(job_id,name,session_id,task_title,task_objective,execution_mode,skill_ids,model_ref,interval_seconds,next_run_at,timeout_seconds,max_retries,backoff_seconds,misfire_policy,idempotency_key,status,retry_attempt,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		jobID, request.Name, request.SessionID, request.TaskTitle, request.TaskObjective, request.ExecutionMode, skillJSON, request.ModelRef,
		request.IntervalSeconds, formatTime(nextRun), request.TimeoutSeconds, request.MaxRetries, request.BackoffSeconds,
		request.MisfirePolicy, request.IdempotencyKey, schedulerapi.StatusActive, 0, when, when)
	if err != nil {
		if sqliteerror.IsUniqueConstraint(err) {
			return s.getByIdempotencyKey(ctx, request.IdempotencyKey)
		}
		return schedulerapi.Job{}, err
	}
	return s.Get(ctx, jobID)
}

func (s *Store) Get(ctx context.Context, jobID string) (schedulerapi.Job, error) {
	jobID = strings.TrimSpace(jobID)
	if jobID == "" {
		return schedulerapi.Job{}, errors.New("job_id is required")
	}
	job, err := scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobSelectColumns+` FROM jobs WHERE job_id=?`, jobID))
	if errors.Is(err, sql.ErrNoRows) {
		return schedulerapi.Job{}, schedulerapi.ErrNotFound
	}
	return job, err
}

func (s *Store) getByIdempotencyKey(ctx context.Context, key string) (schedulerapi.Job, error) {
	return scanJob(s.db.QueryRowContext(ctx, `SELECT `+jobSelectColumns+` FROM jobs WHERE idempotency_key=?`, key))
}

func (s *Store) List(ctx context.Context, includeArchived bool) ([]schedulerapi.Job, error) {
	query := `SELECT ` + jobSelectColumns + ` FROM jobs`
	if !includeArchived {
		query += ` WHERE status <> 'archived'`
	}
	query += ` ORDER BY next_run_at,job_id`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []schedulerapi.Job
	for rows.Next() {
		job, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) ChangeState(ctx context.Context, request schedulerapi.StateRequest, status string) (schedulerapi.Job, error) {
	request.JobID = strings.TrimSpace(request.JobID)
	request.Reviewer = strings.TrimSpace(request.Reviewer)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.JobID == "" || request.Reviewer == "" || request.Reason == "" {
		return schedulerapi.Job{}, errors.New("job_id, reviewer and reason are required")
	}
	if status != schedulerapi.StatusPaused && status != schedulerapi.StatusActive && status != schedulerapi.StatusArchived {
		return schedulerapi.Job{}, errors.New("invalid job state")
	}
	result, err := s.db.ExecContext(ctx, `UPDATE jobs SET status=?,updated_at=? WHERE job_id=? AND status <> 'archived'`, status, nowString(), request.JobID)
	if err != nil {
		return schedulerapi.Job{}, err
	}
	if changed, _ := result.RowsAffected(); changed == 0 {
		return schedulerapi.Job{}, errors.New("job state was not changed")
	}
	return s.Get(ctx, request.JobID)
}

func (s *Store) ClaimDue(ctx context.Context, request schedulerapi.ClaimDueRequest) ([]schedulerapi.TaskRequest, error) {
	request.Owner = strings.TrimSpace(request.Owner)
	if request.Owner == "" {
		return nil, errors.New("claim owner is required")
	}
	if request.Limit <= 0 || request.Limit > 100 {
		request.Limit = 10
	}
	if request.LeaseSeconds <= 0 || request.LeaseSeconds > 3600 {
		request.LeaseSeconds = 60
	}
	now, err := parseOptionalTime(request.Now, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	type expiredRun struct {
		runID       string
		jobID       string
		attempt     int
		maxRetries  int
		backoff     int64
		scheduledAt string
	}
	expiredRows, err := tx.QueryContext(ctx, `SELECT r.run_id,r.job_id,r.attempt,j.max_retries,j.backoff_seconds,r.scheduled_at FROM job_runs r JOIN job_leases l ON l.run_id=r.run_id JOIN jobs j ON j.job_id=r.job_id WHERE l.expires_at<=? AND r.status='claimed'`, formatTime(now))
	if err != nil {
		return nil, err
	}
	var expired []expiredRun
	for expiredRows.Next() {
		var item expiredRun
		if err := expiredRows.Scan(&item.runID, &item.jobID, &item.attempt, &item.maxRetries, &item.backoff, &item.scheduledAt); err != nil {
			_ = expiredRows.Close()
			return nil, err
		}
		expired = append(expired, item)
	}
	if err := expiredRows.Close(); err != nil {
		return nil, err
	}
	for _, item := range expired {
		if _, err := tx.ExecContext(ctx, `UPDATE job_runs SET status='timed_out',finished_at=?,error='lease expired' WHERE run_id=? AND status='claimed'`, formatTime(now), item.runID); err != nil {
			return nil, err
		}
		if item.attempt <= item.maxRetries {
			retryAt := now.Add(time.Duration(item.backoff*int64(item.attempt)) * time.Second)
			if _, err := tx.ExecContext(ctx, `UPDATE jobs SET retry_attempt=?,retry_at=?,retry_origin_at=?,updated_at=? WHERE job_id=?`, item.attempt, formatTime(retryAt), item.scheduledAt, formatTime(now), item.jobID); err != nil {
				return nil, err
			}
		} else if _, err := tx.ExecContext(ctx, `UPDATE jobs SET retry_attempt=0,retry_at='',retry_origin_at='',updated_at=? WHERE job_id=?`, formatTime(now), item.jobID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM job_leases WHERE expires_at<=?`, formatTime(now)); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT job_id,name,session_id,task_title,task_objective,execution_mode,skill_ids,model_ref,interval_seconds,next_run_at,timeout_seconds,max_retries,backoff_seconds,misfire_policy,idempotency_key,status,retry_attempt,retry_at,retry_origin_at,created_at,updated_at FROM jobs WHERE status='active' AND (CASE WHEN retry_at<>'' THEN retry_at ELSE next_run_at END)<=? AND job_id NOT IN (SELECT job_id FROM job_leases) ORDER BY (CASE WHEN retry_at<>'' THEN retry_at ELSE next_run_at END),job_id LIMIT ?`, formatTime(now), request.Limit)
	if err != nil {
		return nil, err
	}
	type dueJob struct {
		job          schedulerapi.Job
		retryAttempt int
		retryAt      string
		retryOrigin  string
	}
	var due []dueJob
	for rows.Next() {
		var item dueJob
		if err := scanDueJob(rows, &item.job, &item.retryAttempt, &item.retryAt, &item.retryOrigin); err != nil {
			_ = rows.Close()
			return nil, err
		}
		due = append(due, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var tasks []schedulerapi.TaskRequest
	for _, item := range due {
		scheduledAt, _ := time.Parse(time.RFC3339Nano, item.job.NextRunAt)
		if item.retryAttempt > 0 && item.retryOrigin != "" {
			scheduledAt, _ = time.Parse(time.RFC3339Nano, item.retryOrigin)
		}
		attempt := item.retryAttempt + 1
		runID, _ := newID("run")
		leaseID, _ := newID("lease")
		deliveryKey := fmt.Sprintf("%s/%s/%d", item.job.IdempotencyKey, formatTime(scheduledAt), attempt)
		coreTaskKey := fmt.Sprintf("%s/%s", item.job.IdempotencyKey, formatTime(scheduledAt))
		started := formatTime(now)
		expires := formatTime(now.Add(time.Duration(request.LeaseSeconds) * time.Second))
		if _, err := tx.ExecContext(ctx, `INSERT INTO job_runs(run_id,job_id,scheduled_at,attempt,status,task_request_key,core_task_key,started_at) VALUES(?,?,?,?,?,?,?,?)`, runID, item.job.ID, formatTime(scheduledAt), attempt, "claimed", deliveryKey, coreTaskKey, started); err != nil {
			if sqliteerror.IsUniqueConstraint(err) {
				continue
			}
			return nil, err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO job_leases(job_id,run_id,lease_id,owner,acquired_at,expires_at) VALUES(?,?,?,?,?,?)`, item.job.ID, runID, leaseID, request.Owner, started, expires); err != nil {
			return nil, err
		}
		if item.retryAttempt > 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE jobs SET retry_at='',updated_at=? WHERE job_id=?`, started, item.job.ID); err != nil {
				return nil, err
			}
		} else {
			nextRun := advanceNextRun(scheduledAt, now, time.Duration(item.job.IntervalSeconds)*time.Second, item.job.MisfirePolicy)
			if _, err := tx.ExecContext(ctx, `UPDATE jobs SET next_run_at=?,updated_at=? WHERE job_id=?`, formatTime(nextRun), started, item.job.ID); err != nil {
				return nil, err
			}
		}
		tasks = append(tasks, schedulerapi.TaskRequest{
			JobID: item.job.ID, RunID: runID, LeaseID: leaseID, SessionID: item.job.SessionID,
			Title: item.job.TaskTitle, Objective: item.job.TaskObjective,
			ExecutionMode: item.job.ExecutionMode, SkillIDs: append([]string(nil), item.job.SkillIDs...),
			ModelRef:    item.job.ModelRef,
			ScheduledAt: formatTime(scheduledAt), TimeoutSeconds: item.job.TimeoutSeconds, IdempotencyKey: coreTaskKey,
		})
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return tasks, nil
}

func (s *Store) Acknowledge(ctx context.Context, request schedulerapi.AcknowledgeRequest) (bool, error) {
	request.RunID = strings.TrimSpace(request.RunID)
	request.LeaseID = strings.TrimSpace(request.LeaseID)
	request.CoreTaskID = strings.TrimSpace(request.CoreTaskID)
	request.Status = strings.ToLower(strings.TrimSpace(request.Status))
	request.Error = strings.TrimSpace(request.Error)
	if request.RunID == "" || request.LeaseID == "" {
		return false, errors.New("run_id and lease_id are required")
	}
	allowed := map[string]bool{"task_created": true, "waiting_approval": true, "completed": true, "failed": true, "timed_out": true, "cancelled": true}
	if !allowed[request.Status] {
		return false, errors.New("invalid run status")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var jobID, scheduledAt string
	var attempt, maxRetries int
	var backoff int64
	if err := tx.QueryRowContext(ctx, `SELECT r.job_id,r.scheduled_at,r.attempt,j.max_retries,j.backoff_seconds FROM job_runs r JOIN job_leases l ON l.run_id=r.run_id JOIN jobs j ON j.job_id=r.job_id WHERE r.run_id=? AND l.lease_id=?`, request.RunID, request.LeaseID).Scan(&jobID, &scheduledAt, &attempt, &maxRetries, &backoff); err != nil {
		return false, err
	}
	finished := ""
	terminal := request.Status == "completed" || request.Status == "failed" || request.Status == "timed_out" || request.Status == "cancelled"
	coreOwned := request.Status == "task_created" || request.Status == "waiting_approval"
	if coreOwned && request.CoreTaskID == "" {
		return false, errors.New("core_task_id is required after Core accepts a task")
	}
	if terminal {
		finished = nowString()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE job_runs SET status=?,core_task_id=?,error=?,finished_at=? WHERE run_id=?`, request.Status, request.CoreTaskID, request.Error, finished, request.RunID); err != nil {
		return false, err
	}
	if terminal || coreOwned {
		if _, err := tx.ExecContext(ctx, `DELETE FROM job_leases WHERE lease_id=?`, request.LeaseID); err != nil {
			return false, err
		}
	}
	if coreOwned {
		if _, err := tx.ExecContext(ctx, `UPDATE jobs SET retry_attempt=0,retry_at='',retry_origin_at='',updated_at=? WHERE job_id=?`, nowString(), jobID); err != nil {
			return false, err
		}
	}
	if terminal {
		if request.Status == "failed" || request.Status == "timed_out" {
			if attempt <= maxRetries {
				retryAt := time.Now().UTC().Add(time.Duration(backoff*int64(attempt)) * time.Second)
				if _, err := tx.ExecContext(ctx, `UPDATE jobs SET retry_attempt=?,retry_at=?,retry_origin_at=?,updated_at=? WHERE job_id=?`, attempt, formatTime(retryAt), scheduledAt, nowString(), jobID); err != nil {
					return false, err
				}
			} else {
				_, _ = tx.ExecContext(ctx, `UPDATE jobs SET retry_attempt=0,retry_at='',retry_origin_at='',updated_at=? WHERE job_id=?`, nowString(), jobID)
			}
		} else {
			_, _ = tx.ExecContext(ctx, `UPDATE jobs SET retry_attempt=0,updated_at=? WHERE job_id=?`, nowString(), jobID)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func advanceNextRun(scheduledAt, now time.Time, interval time.Duration, policy string) time.Time {
	next := scheduledAt.Add(interval)
	switch policy {
	case schedulerapi.MisfireCatchUp:
		return next
	case schedulerapi.MisfireSkip:
		for !next.After(now) {
			next = next.Add(interval)
		}
		return next
	default:
		if next.Before(now) {
			return now.Add(interval)
		}
		return next
	}
}

type scanner interface{ Scan(...any) error }

func scanJob(row scanner) (schedulerapi.Job, error) {
	var job schedulerapi.Job
	var skillJSON string
	err := row.Scan(&job.ID, &job.Name, &job.SessionID, &job.TaskTitle, &job.TaskObjective, &job.ExecutionMode, &skillJSON, &job.ModelRef,
		&job.IntervalSeconds, &job.NextRunAt, &job.TimeoutSeconds, &job.MaxRetries, &job.BackoffSeconds, &job.MisfirePolicy,
		&job.IdempotencyKey, &job.Status, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return schedulerapi.Job{}, err
	}
	job.ExecutionMode = normalizeExecutionMode(job.ExecutionMode)
	job.ModelRef = strings.TrimSpace(job.ModelRef)
	job.SkillIDs, err = decodeSkillIDs(skillJSON)
	if err != nil {
		return schedulerapi.Job{}, err
	}
	return job, nil
}

func scanDueJob(row scanner, job *schedulerapi.Job, retry *int, retryAt, retryOrigin *string) error {
	var skillJSON string
	if err := row.Scan(&job.ID, &job.Name, &job.SessionID, &job.TaskTitle, &job.TaskObjective, &job.ExecutionMode, &skillJSON, &job.ModelRef,
		&job.IntervalSeconds, &job.NextRunAt, &job.TimeoutSeconds, &job.MaxRetries, &job.BackoffSeconds, &job.MisfirePolicy,
		&job.IdempotencyKey, &job.Status, retry, retryAt, retryOrigin, &job.CreatedAt, &job.UpdatedAt); err != nil {
		return err
	}
	job.ExecutionMode = normalizeExecutionMode(job.ExecutionMode)
	job.ModelRef = strings.TrimSpace(job.ModelRef)
	ids, err := decodeSkillIDs(skillJSON)
	if err != nil {
		return err
	}
	job.SkillIDs = ids
	return nil
}

func normalizeExecutionMode(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case schedulerapi.ExecutionModePlan:
		return schedulerapi.ExecutionModePlan
	default:
		return schedulerapi.ExecutionModeAgent
	}
}

func normalizeSkillIDs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	out := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func encodeSkillIDs(ids []string) (string, error) {
	if len(ids) == 0 {
		return "[]", nil
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeSkillIDs(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil, nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil, fmt.Errorf("decode skill_ids: %w", err)
	}
	return normalizeSkillIDs(ids), nil
}

func parseOptionalTime(raw string, fallback time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback.UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, errors.New("time must use RFC3339Nano")
	}
	return parsed.UTC(), nil
}
func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
func nowString() string                 { return formatTime(time.Now().UTC()) }
func newID(prefix string) (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(value[:]), nil
}
