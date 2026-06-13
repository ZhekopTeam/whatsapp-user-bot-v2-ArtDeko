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
			&job.TaskID,
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
