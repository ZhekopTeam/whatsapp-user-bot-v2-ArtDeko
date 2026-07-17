package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"time"

	"my-whatsapp-bot/wa-user-bot/domain"
	"my-whatsapp-bot/wa-user-bot/storage/sqlite"
	"my-whatsapp-bot/wa-user-bot/templates"
)

type MessageSender interface {
	SendText(ctx context.Context, senderPhone string, receiverPhone string, text string) error
}

type cooldownState struct {
	consecutiveFailures int
	cooldownUntil       time.Time
}

type Dispatcher struct {
	jobsRepo      *sqlite.JobsRepo
	accountsRepo  *sqlite.AccountsRepo
	adminRepo     *sqlite.AdminProxyRepo
	sender        MessageSender
	generator     *templates.Generator
	batchSize     int
	location      *time.Location
	windowEndHour int
	replyDelayMin int
	replyDelayMax int
	rand          *rand.Rand

	mu        sync.Mutex
	rebuildMu sync.Mutex
	cooldowns map[string]cooldownState
}

func NewDispatcher(
	jobsRepo *sqlite.JobsRepo,
	accountsRepo *sqlite.AccountsRepo,
	adminRepo *sqlite.AdminProxyRepo,
	sender MessageSender,
	generator *templates.Generator,
	batchSize int,
	location *time.Location,
	windowEndHour int,
	replyDelayMin int,
	replyDelayMax int,
) *Dispatcher {
	if location == nil {
		location = time.UTC
	}
	if windowEndHour <= 0 || windowEndHour > 23 {
		windowEndHour = 22
	}
	if replyDelayMin <= 0 {
		replyDelayMin = 12
	}
	if replyDelayMax < replyDelayMin {
		replyDelayMax = replyDelayMin
	}
	return &Dispatcher{
		jobsRepo:      jobsRepo,
		accountsRepo:  accountsRepo,
		adminRepo:     adminRepo,
		sender:        sender,
		generator:     generator,
		batchSize:     batchSize,
		location:      location,
		windowEndHour: windowEndHour,
		replyDelayMin: replyDelayMin,
		replyDelayMax: replyDelayMax,
		rand:          rand.New(rand.NewSource(time.Now().UnixNano())),
		cooldowns:     make(map[string]cooldownState),
	}
}

func (d *Dispatcher) DispatchDue(ctx context.Context, now time.Time) error {
	cancelled, err := d.jobsRepo.CancelOutsideSendWindow(ctx, now, d.location, d.windowEndHour)
	if err != nil {
		log.Printf("[dispatcher] cancel outside window: %v", err)
	} else if cancelled > 0 {
		log.Printf("[dispatcher] cancelled %d job(s) outside send window", cancelled)
	}

	jobs, err := d.jobsRepo.ClaimDueJobs(ctx, now, d.batchSize)
	if err != nil {
		return fmt.Errorf("claim due jobs: %w", err)
	}

	rebuiltComms := make(map[int64]struct{})

	for _, job := range jobs {
		if _, ok := rebuiltComms[job.CommID]; ok {
			// Already cancelled during ring rebuild in this batch.
			_ = d.jobsRepo.MarkCancelled(ctx, job.ID, "cancelled: ring rebuilt")
			continue
		}

		if d.isPastSendWindow(now, job.PlannedAt) {
			log.Printf("[dispatcher] job %d past send window, cancelling", job.ID)
			if cancelErr := d.jobsRepo.MarkCancelled(ctx, job.ID, "cancelled: past daily send window"); cancelErr != nil {
				log.Printf("failed to cancel job %d: %v", job.ID, cancelErr)
			}
			continue
		}

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

		// Dead account → rebuild ring among survivors (bridge former neighbours).
		if isAccountUnavailable(senderAccount.Status) {
			for _, id := range d.excludeAccountFromDialogue(ctx, senderAccount.AccountID, senderAccount.Phone,
				fmt.Sprintf("cancelled: sender unavailable (%s)", senderAccount.Status)) {
				rebuiltComms[id] = struct{}{}
			}
			continue
		}
		if isAccountUnavailable(receiverAccount.Status) {
			for _, id := range d.excludeAccountFromDialogue(ctx, receiverAccount.AccountID, receiverAccount.Phone,
				fmt.Sprintf("cancelled: receiver unavailable (%s)", receiverAccount.Status)) {
				rebuiltComms[id] = struct{}{}
			}
			continue
		}

		cooldownUntil := d.getCooldown(senderAccount.Phone)
		if now.Before(cooldownUntil) {
			log.Printf("[dispatcher] Sender %s is cooling down. Rescheduling job %d to %v", senderAccount.Phone, job.ID, cooldownUntil)
			if rescheduleErr := d.jobsRepo.RescheduleJob(ctx, job.ID, cooldownUntil); rescheduleErr != nil {
				log.Printf("failed to reschedule job %d: %v", job.ID, rescheduleErr)
			}
			continue
		}

		if err := d.sender.SendText(ctx, senderAccount.Phone, receiverAccount.Phone, job.MessageText); err != nil {
			if isFatalSendError(err) {
				d.markAdminSessionLost(ctx, senderAccount.Phone)
				for _, id := range d.excludeAccountFromDialogue(ctx, senderAccount.AccountID, senderAccount.Phone,
					fmt.Sprintf("cancelled: send failed (%v)", err)) {
					rebuiltComms[id] = struct{}{}
				}
				continue
			}
			if d.recordFailure(ctx, senderAccount.Phone) {
				d.markAdminSessionLost(ctx, senderAccount.Phone)
				for _, id := range d.excludeAccountFromDialogue(ctx, senderAccount.AccountID, senderAccount.Phone,
					fmt.Sprintf("cancelled: send failed (%v)", err)) {
					rebuiltComms[id] = struct{}{}
				}
				continue
			}
			d.markFailed(ctx, job.ID, err)
			log.Printf(
				"[dispatcher] send failed job=%d %s → %s: %v",
				job.ID, senderAccount.Phone, receiverAccount.Phone, err,
			)
			continue
		}

		d.recordSuccess(senderAccount.Phone)

		if err := d.jobsRepo.MarkSent(ctx, job.ID, time.Now().UTC()); err != nil {
			return fmt.Errorf("mark job %d as sent: %w", job.ID, err)
		}
		log.Printf(
			"[dispatcher] sent ok job=%d comm=%d step=%d %s → %s",
			job.ID, job.CommID, job.StepNo,
			senderAccount.Phone, receiverAccount.Phone,
		)
	}

	return nil
}

// ExcludeAccountByPhone rebuilds dialogue rings without this phone (e.g. logout).
func (d *Dispatcher) ExcludeAccountByPhone(ctx context.Context, phone, reason string) {
	account, err := d.accountsRepo.GetByPhone(ctx, phone)
	if err != nil {
		log.Printf("[dispatcher] exclude by phone %s: lookup failed: %v", phone, err)
		return
	}
	d.excludeAccountFromDialogue(ctx, account.AccountID, phone, reason)
}

func (d *Dispatcher) excludeAccountFromDialogue(ctx context.Context, accountID int64, phone, reason string) []int64 {
	log.Printf("[dispatcher] excluding account %d (%s) and rebuilding rings — %s", accountID, phone, reason)
	return d.rebuildRingAfterDrop(ctx, accountID, reason)
}

func isAccountUnavailable(status string) bool {
	switch status {
	case domain.AccountStatusDisconnected,
		domain.AccountStatusFailed,
		domain.AccountStatusBlocked,
		domain.AccountStatusAuthRequired:
		return true
	default:
		return false
	}
}

func isFatalSendError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	fatalHints := []string{
		"logged out",
		"not connected",
		"no device",
		"session",
		"unauthorized",
		"401",
		"websocket",
		"no signal session",
	}
	for _, hint := range fatalHints {
		if strings.Contains(msg, hint) {
			return true
		}
	}
	return false
}

// recordFailure updates cooldown. Returns true if account should be excluded from dialogue
// (too many consecutive failures → marked disconnected).
func (d *Dispatcher) recordFailure(ctx context.Context, phone string) bool {
	d.mu.Lock()
	state := d.cooldowns[phone]
	state.consecutiveFailures++
	failures := state.consecutiveFailures

	var cooldown time.Duration
	exclude := false
	switch failures {
	case 1:
		cooldown = 1 * time.Minute
	case 2:
		cooldown = 5 * time.Minute
	case 3:
		cooldown = 15 * time.Minute
	case 4:
		cooldown = 1 * time.Hour
	default:
		cooldown = 24 * time.Hour
		exclude = true
	}
	state.cooldownUntil = time.Now().Add(cooldown)
	d.cooldowns[phone] = state
	d.mu.Unlock()

	if exclude {
		log.Printf("[dispatcher] Account %s failed 5 consecutive times, marking as disconnected in DB", phone)
		if err := d.accountsRepo.UpdateStatusByPhone(ctx, phone, domain.AccountStatusDisconnected); err != nil {
			log.Printf("Failed to update status for phone %s to disconnected: %v", phone, err)
		}
	}
	log.Printf("[dispatcher] Cooldown updated for %s: failures=%d, until=%v", phone, failures, state.cooldownUntil)
	return exclude
}

func (d *Dispatcher) markAdminSessionLost(ctx context.Context, phone string) {
	if d.adminRepo == nil {
		return
	}
	if err := d.adminRepo.SetAccountStatusByPhone(ctx, phone, "revoked"); err != nil {
		if err != sql.ErrNoRows {
			log.Printf("[dispatcher] mark admin account %s revoked: %v", phone, err)
		}
		return
	}
	log.Printf("[dispatcher] admin account %s marked revoked (session lost)", phone)
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

func (d *Dispatcher) isPastSendWindow(now time.Time, plannedAt time.Time) bool {
	localNow := now.In(d.location)
	localPlanned := plannedAt.In(d.location)

	plannedDayEnd := time.Date(
		localPlanned.Year(), localPlanned.Month(), localPlanned.Day(),
		d.windowEndHour, 0, 0, 0, d.location,
	)
	if !localPlanned.Before(plannedDayEnd) {
		return true
	}
	if localNow.Year() == localPlanned.Year() &&
		localNow.YearDay() == localPlanned.YearDay() &&
		!localNow.Before(plannedDayEnd) {
		return true
	}
	if localPlanned.Before(time.Date(
		localNow.Year(), localNow.Month(), localNow.Day(),
		0, 0, 0, 0, d.location,
	)) {
		return true
	}
	return false
}
