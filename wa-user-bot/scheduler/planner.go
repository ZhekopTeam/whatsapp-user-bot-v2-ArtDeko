package scheduler

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"my-whatsapp-bot/wa-user-bot/domain"
	"my-whatsapp-bot/wa-user-bot/storage/sqlite"
	"my-whatsapp-bot/wa-user-bot/templates"
)

const dialogueMessageCount = 6

type Planner struct {
	communicationsRepo *sqlite.CommunicationsRepo
	jobsRepo           *sqlite.JobsRepo
	generator          *templates.Generator
	rand               *rand.Rand
	location           *time.Location
	windowStart        string
	windowEnd          string
	replyDelayMin      int
	replyDelayMax      int
}

func NewPlanner(
	communicationsRepo *sqlite.CommunicationsRepo,
	jobsRepo *sqlite.JobsRepo,
	generator *templates.Generator,
	location *time.Location,
	windowStart string,
	windowEnd string,
	replyDelayMin int,
	replyDelayMax int,
) *Planner {
	return &Planner{
		communicationsRepo: communicationsRepo,
		jobsRepo:           jobsRepo,
		generator:          generator,
		rand:               rand.New(rand.NewSource(time.Now().UnixNano())),
		location:           location,
		windowStart:        windowStart,
		windowEnd:          windowEnd,
		replyDelayMin:      replyDelayMin,
		replyDelayMax:      replyDelayMax,
	}
}

func (p *Planner) Plan(ctx context.Context, now time.Time) error {
	day := now.In(p.location)
	communications, err := p.communicationsRepo.ListEnabledForDate(ctx, day)
	if err != nil {
		return fmt.Errorf("list enabled communications: %w", err)
	}

	log.Printf("[planner] date=%s found %d enabled communications", day.Format(domain.CommunicationDateLayout), len(communications))

	for _, communication := range communications {
		if !p.shouldPlanOnDay(communication, day) {
			log.Printf("[planner] task %d skipped by shouldPlanOnDay (start=%s end=%s countDays=%d)",
				communication.TaskID,
				communication.StartDate.Format(domain.CommunicationDateLayout),
				communication.EndDate.Format(domain.CommunicationDateLayout),
				communication.CountDays,
			)
			continue
		}

		runDate := normalizeDay(day)
		firstMessageAt, err := p.pickFirstMessageTime(runDate, now)
		if err != nil {
			return err
		}
		jobs, err := p.buildJobs(communication, runDate, firstMessageAt)
		if err != nil {
			return fmt.Errorf("build jobs for task %d: %w", communication.TaskID, err)
		}

		run := domain.CommunicationRun{
			TaskID:    communication.TaskID,
			RunDate:   runDate,
			Status:    domain.RunStatusPlanned,
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		}
		if err := p.jobsRepo.CreateRunWithJobs(ctx, run, jobs); err != nil {
			return fmt.Errorf("create run for task %d: %w", communication.TaskID, err)
		}
	}

	return nil
}

func (p *Planner) shouldPlanOnDay(communication domain.Communication, day time.Time) bool {
	if !communication.IncludesDate(day) {
		return false
	}
	for _, selectedDay := range selectScheduledDates(communication.StartDate, communication.EndDate, communication.CountDays) {
		if normalizeDay(selectedDay).Equal(normalizeDay(day)) {
			return true
		}
	}
	return false
}

func (p *Planner) pickFirstMessageTime(day time.Time, now time.Time) (time.Time, error) {
	startHour, startMinute, err := parseClock(p.windowStart)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse first message window start: %w", err)
	}
	endHour, endMinute, err := parseClock(p.windowEnd)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse first message window end: %w", err)
	}

	start := time.Date(day.Year(), day.Month(), day.Day(), startHour, startMinute, 0, 0, p.location)
	end := time.Date(day.Year(), day.Month(), day.Day(), endHour, endMinute, 0, 0, p.location)

	nowInLocation := now.In(p.location)
	if nowInLocation.After(start) {
		start = nowInLocation.Add(time.Minute)
	}

	if !end.After(start) {
		return time.Time{}, fmt.Errorf("first message window has already passed for today")
	}

	spanMinutes := int(end.Sub(start).Minutes())
	offset := p.rand.Intn(spanMinutes + 1)
	return start.Add(time.Duration(offset) * time.Minute), nil
}

func (p *Planner) buildJobs(communication domain.Communication, runDate time.Time, firstMessageAt time.Time) ([]domain.MessageJob, error) {
	offsets, err := buildUniqueMinuteOffsets(p.rand, dialogueMessageCount-1, p.replyDelayMin, p.replyDelayMax)
	if err != nil {
		return nil, err
	}

	jobs := make([]domain.MessageJob, 0, dialogueMessageCount)
	plannedAt := firstMessageAt
	now := time.Now().UTC()
	for index := 0; index < dialogueMessageCount; index++ {
		senderID := communication.Account1
		receiverID := communication.Account2
		if index%2 == 1 {
			senderID = communication.Account2
			receiverID = communication.Account1
		}

		jobs = append(jobs, domain.MessageJob{
			TaskID:            communication.TaskID,
			RunDate:           runDate,
			StepNo:            index + 1,
			SenderAccountID:   senderID,
			ReceiverAccountID: receiverID,
			PlannedAt:         plannedAt.UTC(),
			Status:            domain.JobStatusPending,
			MessageText:       p.generator.BuildMessage(),
			CreatedAt:         now,
			UpdatedAt:         now,
		})

		if index < len(offsets) {
			plannedAt = plannedAt.Add(time.Duration(offsets[index]) * time.Minute)
		}
	}

	return jobs, nil
}

func parseClock(value string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid clock format %q", value)
	}
	hour, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	minute, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("clock value out of range")
	}
	return hour, minute, nil
}
