package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"my-whatsapp-bot/wa-user-bot/domain"
)

type JobsRepo struct {
	db *sql.DB
}

func NewJobsRepo(db *sql.DB) *JobsRepo {
	return &JobsRepo{db: db}
}

func (r *JobsRepo) CreateRunWithJobs(ctx context.Context, run domain.CommunicationRun, jobs []domain.Message) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin jobs tx: %w", err)
	}

	runDate := run.RunDate.Format(domain.CommunicationDateLayout)
	if _, err := tx.ExecContext(ctx, `
		INSERT OR IGNORE INTO communication_runs (comm_id, run_date, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, run.CommID, runDate, run.Status, run.CreatedAt, run.UpdatedAt); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("insert communication run: %w", err)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT OR IGNORE INTO message_jobs (
			comm_id, run_date, step_no, sender_account_id, receiver_account_id,
			planned_at, status, message_text, attempt_count, last_error, sent_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("prepare message jobs insert: %w", err)
	}
	defer stmt.Close()

	for _, job := range jobs {
		if _, err := stmt.ExecContext(
			ctx,
			job.CommID,
			job.RunDate.Format(domain.CommunicationDateLayout),
			job.StepNo,
			job.SenderAccountID,
			job.ReceiverAccountID,
			job.PlannedAt,
			job.Status,
			job.MessageText,
			job.AttemptCount,
			job.LastError,
			job.SentAt,
			job.CreatedAt,
			job.UpdatedAt,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("insert message job step %d: %w", job.StepNo, err)
		}
	}

	return tx.Commit()
}

func (r *JobsRepo) ClaimDueJobs(ctx context.Context, now time.Time, limit int) ([]domain.Message, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin claim jobs tx: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT id, comm_id, run_date, step_no, sender_account_id, receiver_account_id,
			planned_at, status, message_text, attempt_count, last_error, sent_at, created_at, updated_at
		FROM message_jobs
		WHERE status = ? AND planned_at <= ?
		ORDER BY planned_at, id
		LIMIT ?
	`, domain.JobStatusPending, now.UTC(), limit)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("query due jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]domain.Message, 0)
	ids := make([]int64, 0)
	for rows.Next() {
		var job domain.Message
		var runDate string
		var sentAt sql.NullTime
		if err := rows.Scan(
			&job.ID,
			&job.CommID,
			&runDate,
			&job.StepNo,
			&job.SenderAccountID,
			&job.ReceiverAccountID,
			&job.PlannedAt,
			&job.Status,
			&job.MessageText,
			&job.AttemptCount,
			&job.LastError,
			&sentAt,
			&job.CreatedAt,
			&job.UpdatedAt,
		); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("scan due job: %w", err)
		}
		parsedRunDate, err := time.Parse(domain.CommunicationDateLayout, runDate)
		if err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("parse run date: %w", err)
		}
		job.RunDate = parsedRunDate
		if sentAt.Valid {
			job.SentAt = &sentAt.Time
		}
		jobs = append(jobs, job)
		ids = append(ids, job.ID)
	}

	if err := rows.Err(); err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("iterate due jobs: %w", err)
	}

	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `
			UPDATE message_jobs
			SET status = ?, attempt_count = attempt_count + 1, updated_at = ?
			WHERE id = ? AND status = ?
		`, domain.JobStatusSending, now.UTC(), id, domain.JobStatusPending); err != nil {
			_ = tx.Rollback()
			return nil, fmt.Errorf("mark job %d as sending: %w", id, err)
		}
		for index := range jobs {
			if jobs[index].ID == id {
				jobs[index].Status = domain.JobStatusSending
				jobs[index].AttemptCount++
				jobs[index].UpdatedAt = now.UTC()
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit claim jobs tx: %w", err)
	}

	return jobs, nil
}

func (r *JobsRepo) MarkSent(ctx context.Context, jobID int64, sentAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE message_jobs
		SET status = ?, sent_at = ?, updated_at = ?, last_error = ''
		WHERE id = ?
	`, domain.JobStatusSent, sentAt.UTC(), sentAt.UTC(), jobID)
	if err != nil {
		return fmt.Errorf("mark job as sent: %w", err)
	}
	return nil
}

func (r *JobsRepo) MarkFailed(ctx context.Context, jobID int64, message string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE message_jobs
		SET status = ?, last_error = ?, updated_at = ?
		WHERE id = ?
	`, domain.JobStatusFailed, message, time.Now().UTC(), jobID)
	if err != nil {
		return fmt.Errorf("mark job as failed: %w", err)
	}
	return nil
}

func (r *JobsRepo) MarkCancelled(ctx context.Context, jobID int64, reason string) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE message_jobs
		SET status = ?, last_error = ?, updated_at = ?
		WHERE id = ? AND status IN (?, ?)
	`, domain.JobStatusCancelled, reason, time.Now().UTC(), jobID,
		domain.JobStatusPending, domain.JobStatusSending)
	if err != nil {
		return fmt.Errorf("mark job as cancelled: %w", err)
	}
	return nil
}

// CancelOutsideSendWindow cancels pending/sending jobs whose send-day window
// has already ended. Jobs planned for future calendar days are kept.
// endHour is local (e.g. 22 means 22:00).
func (r *JobsRepo) CancelOutsideSendWindow(
	ctx context.Context,
	now time.Time,
	location *time.Location,
	endHour int,
) (int64, error) {
	if location == nil {
		location = time.UTC
	}
	if endHour <= 0 || endHour > 23 {
		endHour = 22
	}

	localNow := now.In(location)
	todayStart := time.Date(
		localNow.Year(), localNow.Month(), localNow.Day(),
		0, 0, 0, 0, location,
	)
	tomorrowStart := todayStart.AddDate(0, 0, 1)
	todayWindowEnd := time.Date(
		localNow.Year(), localNow.Month(), localNow.Day(),
		endHour, 0, 0, 0, location,
	)

	var total int64

	// Missed previous days entirely.
	res, err := r.db.ExecContext(ctx, `
		UPDATE message_jobs
		SET status = ?, last_error = ?, updated_at = ?
		WHERE status IN (?, ?) AND planned_at < ?
	`, domain.JobStatusCancelled, "cancelled: send window ended", now.UTC(),
		domain.JobStatusPending, domain.JobStatusSending, todayStart.UTC())
	if err != nil {
		return 0, fmt.Errorf("cancel past-day jobs: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		total += n
	}

	// After today's window: cancel remaining jobs planned for today.
	if !localNow.Before(todayWindowEnd) {
		res, err = r.db.ExecContext(ctx, `
			UPDATE message_jobs
			SET status = ?, last_error = ?, updated_at = ?
			WHERE status IN (?, ?) AND planned_at >= ? AND planned_at < ?
		`, domain.JobStatusCancelled, "cancelled: past daily send window", now.UTC(),
			domain.JobStatusPending, domain.JobStatusSending,
			todayStart.UTC(), tomorrowStart.UTC())
		if err != nil {
			return total, fmt.Errorf("cancel today leftover jobs: %w", err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			total += n
		}
	}

	return total, nil
}

func (r *JobsRepo) ResetSendingJobs(ctx context.Context) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE message_jobs
		SET status = ?, updated_at = ?
		WHERE status = ?
	`, domain.JobStatusPending, time.Now().UTC(), domain.JobStatusSending)
	if err != nil {
		return fmt.Errorf("reset sending jobs: %w", err)
	}
	return nil
}

func (r *JobsRepo) RescheduleJob(ctx context.Context, jobID int64, newPlannedAt time.Time) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE message_jobs
		SET status = ?, planned_at = ?, updated_at = ?
		WHERE id = ?
	`, domain.JobStatusPending, newPlannedAt.UTC(), time.Now().UTC(), jobID)
	if err != nil {
		return fmt.Errorf("reschedule job %d: %w", jobID, err)
	}
	return nil
}

// CancelJobsInvolvingAccount cancels all pending/sending jobs where the account
// is sender or receiver. Other accounts' pairs keep running.
func (r *JobsRepo) CancelJobsInvolvingAccount(ctx context.Context, accountID int64, reason string) (int64, error) {
	if reason == "" {
		reason = "cancelled: account excluded from dialogue"
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE message_jobs
		SET status = ?, last_error = ?, updated_at = ?
		WHERE status IN (?, ?)
		  AND (sender_account_id = ? OR receiver_account_id = ?)
	`, domain.JobStatusCancelled, reason, time.Now().UTC(),
		domain.JobStatusPending, domain.JobStatusSending,
		accountID, accountID)
	if err != nil {
		return 0, fmt.Errorf("cancel jobs involving account %d: %w", accountID, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// CancelJobsInvolvingAccountInComm cancels pending/sending jobs for one comm only.
func (r *JobsRepo) CancelJobsInvolvingAccountInComm(ctx context.Context, commID, accountID int64, reason string) (int64, error) {
	if reason == "" {
		reason = "cancelled: account excluded from dialogue"
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE message_jobs
		SET status = ?, last_error = ?, updated_at = ?
		WHERE comm_id = ?
		  AND status IN (?, ?)
		  AND (sender_account_id = ? OR receiver_account_id = ?)
	`, domain.JobStatusCancelled, reason, time.Now().UTC(),
		commID, domain.JobStatusPending, domain.JobStatusSending,
		accountID, accountID)
	if err != nil {
		return 0, fmt.Errorf("cancel jobs involving account %d in comm %d: %w", accountID, commID, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ListCommIDsInvolvingAccount returns distinct comm_ids with pending/sending jobs
// for the given account (as sender or receiver).
func (r *JobsRepo) ListCommIDsInvolvingAccount(ctx context.Context, accountID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT comm_id
		FROM message_jobs
		WHERE status IN (?, ?)
		  AND (sender_account_id = ? OR receiver_account_id = ?)
		ORDER BY comm_id
	`, domain.JobStatusPending, domain.JobStatusSending, accountID, accountID)
	if err != nil {
		return nil, fmt.Errorf("list comms involving account %d: %w", accountID, err)
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetAccountOrderForComm returns account IDs in ring order (first appearance in jobs).
func (r *JobsRepo) GetAccountOrderForComm(ctx context.Context, commID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT sender_account_id, receiver_account_id
		FROM message_jobs
		WHERE comm_id = ?
		ORDER BY run_date, step_no, id
	`, commID)
	if err != nil {
		return nil, fmt.Errorf("get account order for comm %d: %w", commID, err)
	}
	defer rows.Close()

	seen := make(map[int64]struct{})
	order := make([]int64, 0)
	add := func(id int64) {
		if id == 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		order = append(order, id)
	}
	for rows.Next() {
		var sender, receiver int64
		if err := rows.Scan(&sender, &receiver); err != nil {
			return nil, err
		}
		add(sender)
		add(receiver)
	}
	return order, rows.Err()
}

// ListPendingRunDates returns run_date strings that still have pending/sending jobs.
func (r *JobsRepo) ListPendingRunDates(ctx context.Context, commID int64) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT DISTINCT run_date
		FROM message_jobs
		WHERE comm_id = ? AND status IN (?, ?)
		ORDER BY run_date
	`, commID, domain.JobStatusPending, domain.JobStatusSending)
	if err != nil {
		return nil, fmt.Errorf("list pending run dates for comm %d: %w", commID, err)
	}
	defer rows.Close()

	var dates []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		dates = append(dates, d)
	}
	return dates, rows.Err()
}

func (r *JobsRepo) CancelPendingForComm(ctx context.Context, commID int64, reason string) (int64, error) {
	if reason == "" {
		reason = "cancelled: ring rebuild"
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE message_jobs
		SET status = ?, last_error = ?, updated_at = ?
		WHERE comm_id = ? AND status IN (?, ?)
	`, domain.JobStatusCancelled, reason, time.Now().UTC(),
		commID, domain.JobStatusPending, domain.JobStatusSending)
	if err != nil {
		return 0, fmt.Errorf("cancel pending for comm %d: %w", commID, err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (r *JobsRepo) MaxStepNo(ctx context.Context, commID int64, runDate string) (int, error) {
	var max sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT MAX(step_no) FROM message_jobs WHERE comm_id = ? AND run_date = ?
	`, commID, runDate).Scan(&max)
	if err != nil {
		return 0, fmt.Errorf("max step for comm %d date %s: %w", commID, runDate, err)
	}
	if !max.Valid {
		return 0, nil
	}
	return int(max.Int64), nil
}
