package scheduler

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"my-whatsapp-bot/wa-user-bot/domain"
	"my-whatsapp-bot/wa-user-bot/templates"
)

const (
	groupCyclesPerPair = 3
	groupWindowStart   = 10
)

// rebuildRingAfterDrop rebuilds the dialogue ring for remaining accounts in each
// affected ACTIVE communication. Example: [1,2,3,4] drop 3 → pairs among [1,2,4]
// including the new bridge 2↔4.
// Stale leftover tasks (not in enabled groups) only cancel jobs involving the account.
func (d *Dispatcher) rebuildRingAfterDrop(ctx context.Context, droppedAccountID int64, reason string) []int64 {
	if d.generator == nil {
		log.Printf("[dispatcher] ring rebuild skipped: no message generator")
		n, err := d.jobsRepo.CancelJobsInvolvingAccount(ctx, droppedAccountID, reason)
		if err != nil {
			log.Printf("[dispatcher] fallback cancel involving %d: %v", droppedAccountID, err)
			return nil
		}
		log.Printf("[dispatcher] cancelled %d job(s) involving account %d (no rebuild)", n, droppedAccountID)
		return nil
	}

	d.rebuildMu.Lock()
	defer d.rebuildMu.Unlock()

	commIDs, err := d.jobsRepo.ListCommIDsInvolvingAccount(ctx, droppedAccountID)
	if err != nil {
		log.Printf("[dispatcher] list comms for account %d: %v", droppedAccountID, err)
		return nil
	}
	if len(commIDs) == 0 {
		return nil
	}

	activeComms := map[int64]struct{}{}
	if d.adminRepo != nil {
		activeComms, err = d.adminRepo.ListEnabledCommIDs(ctx)
		if err != nil {
			log.Printf("[dispatcher] list enabled comms: %v (treating all as active)", err)
			activeComms = nil
		}
	}

	affected := make([]int64, 0, len(commIDs))
	for _, commID := range commIDs {
		isActive := activeComms == nil
		if activeComms != nil {
			_, isActive = activeComms[commID]
		}
		if !isActive {
			n, cancelErr := d.jobsRepo.CancelJobsInvolvingAccountInComm(ctx, commID, droppedAccountID,
				reason+" (stale comm, no rebuild)")
			if cancelErr != nil {
				log.Printf("[dispatcher] cancel stale jobs for account %d in comm %d: %v",
					droppedAccountID, commID, cancelErr)
			} else {
				log.Printf("[dispatcher] comm %d not in enabled groups: cancelled %d job(s) involving %d (no rebuild)",
					commID, n, droppedAccountID)
			}
			affected = append(affected, commID)
			continue
		}
		if err := d.rebuildCommRing(ctx, commID, droppedAccountID, reason); err != nil {
			log.Printf("[dispatcher] rebuild comm %d after drop %d: %v", commID, droppedAccountID, err)
		}
		affected = append(affected, commID)
	}
	return affected
}

func (d *Dispatcher) rebuildCommRing(ctx context.Context, commID, droppedAccountID int64, reason string) error {
	order, err := d.jobsRepo.GetAccountOrderForComm(ctx, commID)
	if err != nil {
		return err
	}

	survivors := make([]int64, 0, len(order))
	for _, id := range order {
		if id == droppedAccountID {
			continue
		}
		acc, err := d.accountsRepo.GetByID(ctx, id)
		if err != nil {
			log.Printf("[dispatcher] skip account %d in rebuild: %v", id, err)
			continue
		}
		if isAccountUnavailable(acc.Status) {
			continue
		}
		survivors = append(survivors, id)
	}

	runDates, err := d.jobsRepo.ListPendingRunDates(ctx, commID)
	if err != nil {
		return err
	}

	cancelled, err := d.jobsRepo.CancelPendingForComm(ctx, commID, reason)
	if err != nil {
		return err
	}
	log.Printf("[dispatcher] comm %d: cancelled %d pending job(s) after drop %d; survivors=%v",
		commID, cancelled, droppedAccountID, survivors)

	if len(survivors) < 2 {
		log.Printf("[dispatcher] comm %d: fewer than 2 survivors, dialogue stopped", commID)
		return nil
	}

	now := time.Now()
	totalInserted := 0
	for _, runDateStr := range runDates {
		runDate, err := time.ParseInLocation(domain.CommunicationDateLayout, runDateStr, d.location)
		if err != nil {
			log.Printf("[dispatcher] comm %d: bad run_date %q: %v", commID, runDateStr, err)
			continue
		}

		startAt := d.rebuildStartTime(runDate, now)
		jobs := buildGroupJobs(
			commID,
			runDate,
			survivors,
			startAt,
			d.generator,
			d.replyDelayMin,
			d.replyDelayMax,
			d.rand,
		)
		jobs = filterJobsWithinWindow(jobs, runDate, d.location, d.windowEndHour)
		if len(jobs) == 0 {
			log.Printf("[dispatcher] comm %d date %s: no jobs fit window after rebuild", commID, runDateStr)
			continue
		}

		maxStep, err := d.jobsRepo.MaxStepNo(ctx, commID, runDateStr)
		if err != nil {
			return err
		}
		for i := range jobs {
			jobs[i].StepNo = maxStep + i + 1
		}

		run := domain.CommunicationRun{
			CommID:    commID,
			RunDate:   runDate,
			Status:    domain.RunStatusPlanned,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		if err := d.jobsRepo.CreateRunWithJobs(ctx, run, jobs); err != nil {
			return fmt.Errorf("insert rebuilt jobs for %s: %w", runDateStr, err)
		}
		totalInserted += len(jobs)
		log.Printf("[dispatcher] comm %d date %s: rebuilt %d job(s) for %d accounts",
			commID, runDateStr, len(jobs), len(survivors))
	}

	log.Printf("[dispatcher] comm %d ring rebuild done: inserted %d job(s)", commID, totalInserted)
	return nil
}

func (d *Dispatcher) rebuildStartTime(runDate, now time.Time) time.Time {
	localNow := now.In(d.location)
	dayStart := time.Date(runDate.Year(), runDate.Month(), runDate.Day(),
		groupWindowStart, 0, 0, 0, d.location)

	if localNow.Year() == runDate.Year() && localNow.YearDay() == runDate.YearDay() {
		// Continue later today, not from 10:00 again.
		start := localNow.Add(1 * time.Minute)
		if start.Before(dayStart) {
			return dayStart
		}
		return start
	}
	if runDate.After(normalizeDayLocal(localNow, d.location)) {
		return dayStart
	}
	// Past day with leftover pending — start ASAP within that day's window is impossible;
	// use now so filterJobsWithinWindow will drop if past end.
	return localNow.Add(1 * time.Minute)
}

func normalizeDayLocal(t time.Time, loc *time.Location) time.Time {
	t = t.In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func buildDialoguePairs(accountIDs []int64) [][2]int64 {
	n := len(accountIDs)
	if n < 2 {
		return nil
	}
	if n == 2 {
		return [][2]int64{{accountIDs[0], accountIDs[1]}}
	}
	pairs := make([][2]int64, 0, n)
	for i := 0; i < n; i++ {
		pairs = append(pairs, [2]int64{accountIDs[i], accountIDs[(i+1)%n]})
	}
	return pairs
}

func buildGroupJobs(
	commID int64,
	runDate time.Time,
	accountIDs []int64,
	startAt time.Time,
	generator *templates.Generator,
	delayMin, delayMax int,
	rng *rand.Rand,
) []domain.Message {
	if delayMin <= 0 {
		delayMin = 12
	}
	if delayMax < delayMin {
		delayMax = delayMin
	}
	pairs := buildDialoguePairs(accountIDs)
	jobs := make([]domain.Message, 0, len(pairs)*groupCyclesPerPair*2)
	current := startAt.UTC()
	now := time.Now().UTC()
	step := 0

	for _, pair := range pairs {
		accA, accB := pair[0], pair[1]
		for cycle := 0; cycle < groupCyclesPerPair; cycle++ {
			for _, dir := range [][2]int64{{accA, accB}, {accB, accA}} {
				if step > 0 {
					gap := delayMin
					if delayMax > delayMin {
						gap = delayMin + rng.Intn(delayMax-delayMin+1)
					}
					current = current.Add(time.Duration(gap) * time.Minute)
				}
				step++
				jobs = append(jobs, domain.Message{
					CommID:            commID,
					RunDate:           runDate,
					StepNo:            step,
					SenderAccountID:   dir[0],
					ReceiverAccountID: dir[1],
					PlannedAt:         current,
					Status:            domain.JobStatusPending,
					MessageText:       generator.BuildMessage(),
					CreatedAt:         now,
					UpdatedAt:         now,
				})
			}
		}
	}
	return jobs
}

func filterJobsWithinWindow(
	jobs []domain.Message,
	runDate time.Time,
	loc *time.Location,
	windowEndHour int,
) []domain.Message {
	if windowEndHour <= 0 {
		windowEndHour = 22
	}
	day := runDate.In(loc)
	windowEnd := time.Date(day.Year(), day.Month(), day.Day(), windowEndHour, 0, 0, 0, loc)
	kept := make([]domain.Message, 0, len(jobs))
	for _, job := range jobs {
		if !job.PlannedAt.In(loc).Before(windowEnd) {
			continue
		}
		kept = append(kept, job)
	}
	return kept
}
