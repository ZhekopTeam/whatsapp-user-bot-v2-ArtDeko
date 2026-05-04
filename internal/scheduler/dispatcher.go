package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"my-whatsapp-bot/internal/storage/sqlite"
)

type MessageSender interface {
	SendText(ctx context.Context, senderPhone string, receiverPhone string, text string) error
}

type Dispatcher struct {
	jobsRepo     *sqlite.JobsRepo
	accountsRepo *sqlite.AccountsRepo
	sender       MessageSender
	batchSize    int
}

func NewDispatcher(jobsRepo *sqlite.JobsRepo, accountsRepo *sqlite.AccountsRepo, sender MessageSender, batchSize int) *Dispatcher {
	return &Dispatcher{
		jobsRepo:     jobsRepo,
		accountsRepo: accountsRepo,
		sender:       sender,
		batchSize:    batchSize,
	}
}

func (d *Dispatcher) DispatchDue(ctx context.Context, now time.Time) error {
	jobs, err := d.jobsRepo.ClaimDueJobs(ctx, now, d.batchSize)
	if err != nil {
		return fmt.Errorf("claim due jobs: %w", err)
	}

	for _, job := range jobs {
		senderAccount, err := d.accountsRepo.GetByID(ctx, job.SenderAccountID)
		if err != nil {
			d.markFailed(ctx, job.ID, fmt.Errorf("load sender account %d: %w", job.SenderAccountID, err))
			continue
		}
		receiverAccount, err := d.accountsRepo.GetByID(ctx, job.ReceiverAccountID)
		if err != nil {
			d.markFailed(ctx, job.ID, fmt.Errorf("load receiver account %d: %w", job.ReceiverAccountID, err))
			continue
		}
		if senderAccount.Phone == "" || receiverAccount.Phone == "" {
			d.markFailed(ctx, job.ID, errors.New("sender or receiver phone is empty"))
			continue
		}

		if err := d.sender.SendText(ctx, senderAccount.Phone, receiverAccount.Phone, job.MessageText); err != nil {
			d.markFailed(ctx, job.ID, err)
			continue
		}

		if err := d.jobsRepo.MarkSent(ctx, job.ID, time.Now().UTC()); err != nil {
			return fmt.Errorf("mark job %d as sent: %w", job.ID, err)
		}
	}

	return nil
}

func (d *Dispatcher) markFailed(ctx context.Context, jobID int64, err error) {
	if markErr := d.jobsRepo.MarkFailed(ctx, jobID, err.Error()); markErr != nil {
		log.Printf("failed to mark job %d as failed: %v", jobID, markErr)
	}
}
