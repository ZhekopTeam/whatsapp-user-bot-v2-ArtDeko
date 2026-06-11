package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"my-whatsapp-bot/wa-user-bot/domain"
	"my-whatsapp-bot/wa-user-bot/storage/sqlite"
)

type MessageSender interface {
	SendText(ctx context.Context, senderPhone string, receiverPhone string, text string) error
}

type cooldownState struct {
	consecutiveFailures int
	cooldownUntil       time.Time
}

type Dispatcher struct {
	jobsRepo     *sqlite.JobsRepo
	accountsRepo *sqlite.AccountsRepo
	sender       MessageSender
	batchSize    int

	mu        sync.Mutex
	cooldowns map[string]cooldownState
}

func NewDispatcher(jobsRepo *sqlite.JobsRepo, accountsRepo *sqlite.AccountsRepo, sender MessageSender, batchSize int) *Dispatcher {
	return &Dispatcher{
		jobsRepo:     jobsRepo,
		accountsRepo: accountsRepo,
		sender:       sender,
		batchSize:    batchSize,
		cooldowns:    make(map[string]cooldownState),
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

		// Проверяем, не находится ли аккаунт на "остывании" (cooldown)
		cooldownUntil := d.getCooldown(senderAccount.Phone)
		if now.Before(cooldownUntil) {
			log.Printf("[dispatcher] Sender %s is cooling down. Rescheduling job %d to %v", senderAccount.Phone, job.ID, cooldownUntil)
			if rescheduleErr := d.jobsRepo.RescheduleJob(ctx, job.ID, cooldownUntil); rescheduleErr != nil {
				log.Printf("failed to reschedule job %d: %v", job.ID, rescheduleErr)
			}
			continue
		}

		// Проверяем статус отправителя
		if senderAccount.Status != domain.AccountStatusReady {
			d.markFailed(ctx, job.ID, fmt.Errorf("sender account %s is not ready (status: %s)", senderAccount.Phone, senderAccount.Status))
			continue
		}

		if err := d.sender.SendText(ctx, senderAccount.Phone, receiverAccount.Phone, job.MessageText); err != nil {
			d.recordFailure(ctx, senderAccount.Phone)
			d.markFailed(ctx, job.ID, err)
			continue
		}

		d.recordSuccess(senderAccount.Phone)

		if err := d.jobsRepo.MarkSent(ctx, job.ID, time.Now().UTC()); err != nil {
			return fmt.Errorf("mark job %d as sent: %w", job.ID, err)
		}
	}

	return nil
}

func (d *Dispatcher) recordFailure(ctx context.Context, phone string) {
	d.mu.Lock()
	state := d.cooldowns[phone]
	state.consecutiveFailures++

	var cooldown time.Duration
	switch state.consecutiveFailures {
	case 1:
		cooldown = 1 * time.Minute
	case 2:
		cooldown = 5 * time.Minute
	case 3:
		cooldown = 15 * time.Minute
	case 4:
		cooldown = 1 * time.Hour
	default:
		// 5+ consecutive failures: mark status as Disconnected
		cooldown = 24 * time.Hour
		d.mu.Unlock() // unlock to call accounts DB updates
		log.Printf("[dispatcher] Account %s failed 5 consecutive times, marking as disconnected in DB", phone)
		if err := d.accountsRepo.UpdateStatusByPhone(ctx, phone, domain.AccountStatusDisconnected); err != nil {
			log.Printf("Failed to update status for phone %s to disconnected: %v", phone, err)
		}
		d.mu.Lock()
	}

	state.cooldownUntil = time.Now().Add(cooldown)
	d.cooldowns[phone] = state
	d.mu.Unlock()

	log.Printf("[dispatcher] Cooldown updated for %s: failures=%d, until=%v", phone, state.consecutiveFailures, state.cooldownUntil)
}

func (d *Dispatcher) recordSuccess(phone string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.cooldowns, phone)
}

func (d *Dispatcher) getCooldown(phone string) time.Time {
	d.mu.Lock()
	defer d.mu.Unlock()
	state, ok := d.cooldowns[phone]
	if !ok {
		return time.Time{}
	}
	return state.cooldownUntil
}

func (d *Dispatcher) markFailed(ctx context.Context, jobID int64, err error) {
	if markErr := d.jobsRepo.MarkFailed(ctx, jobID, err.Error()); markErr != nil {
		log.Printf("failed to mark job %d as failed: %v", jobID, markErr)
	}
}
