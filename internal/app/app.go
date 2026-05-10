package app

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	"my-whatsapp-bot/internal/config"
	"my-whatsapp-bot/internal/scheduler"
	"my-whatsapp-bot/internal/sheets"
	"my-whatsapp-bot/internal/storage/sqlite"
	"my-whatsapp-bot/internal/templates"
	"my-whatsapp-bot/internal/whatsapp"
)

type App struct {
	settings     *config.Settings
	runtimeDB    *sql.DB
	whatsApp     *whatsapp.Manager
	syncService  *sheets.SyncService
	planner      *scheduler.Planner
	dispatcher   *scheduler.Dispatcher
	accountsRepo *sqlite.AccountsRepo
	sessionsRepo *sqlite.SessionsRepo
	jobsRepo     *sqlite.JobsRepo
}

func New(ctx context.Context, settings *config.Settings) (*App, error) {
	runtimeDB, err := sqlite.Open(settings.RuntimeDBPath)
	if err != nil {
		return nil, fmt.Errorf("open runtime db: %w", err)
	}

	accountsRepo := sqlite.NewAccountsRepo(runtimeDB)
	communicationsRepo := sqlite.NewCommunicationsRepo(runtimeDB)
	jobsRepo := sqlite.NewJobsRepo(runtimeDB)
	sessionsRepo := sqlite.NewSessionsRepo(runtimeDB)
	syncStateRepo := sqlite.NewSyncStateRepo(runtimeDB)

	sheetsClient, err := sheets.NewClient(ctx, settings.CredentialsPath, settings.SpreadsheetID)
	if err != nil {
		_ = runtimeDB.Close()
		return nil, err
	}

	catalog, err := templates.LoadCatalog(settings.SentencesPath)
	if err != nil {
		_ = runtimeDB.Close()
		return nil, err
	}
	generator := templates.NewGenerator(catalog)

	location, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		_ = runtimeDB.Close()
		return nil, fmt.Errorf("load timezone: %w", err)
	}

	manager := whatsapp.NewManager(settings.SessionDBPath)
	manager.OnStatusChange = func(phone, status string) {
		if err := accountsRepo.UpdateStatusByPhone(context.Background(), phone, status); err != nil {
			log.Printf("update account status for phone %s: %v", phone, err)
		}
	}
	sender := whatsapp.NewSender(manager)

	return &App{
		settings:     settings,
		runtimeDB:    runtimeDB,
		whatsApp:     manager,
		accountsRepo: accountsRepo,
		sessionsRepo: sessionsRepo,
		jobsRepo:     jobsRepo,
		syncService: sheets.NewSyncService(
			sheetsClient,
			settings.AccountsSheetName,
			settings.CommunicationsSheetName,
			accountsRepo,
			communicationsRepo,
			syncStateRepo,
		),
		planner: scheduler.NewPlanner(
			communicationsRepo,
			jobsRepo,
			generator,
			location,
			settings.FirstMessageWindowStart,
			settings.FirstMessageWindowEnd,
			settings.ReplyDelayMinMinutes,
			settings.ReplyDelayMaxMinutes,
		),
		dispatcher: scheduler.NewDispatcher(
			jobsRepo,
			accountsRepo,
			sender,
			settings.DispatchBatchSize,
		),
	}, nil
}

func (a *App) Run(ctx context.Context) error {
	if err := a.whatsApp.Start(ctx); err != nil {
		return err
	}

	if err := a.syncService.Sync(ctx); err != nil {
		return err
	}
	if err := a.planner.Plan(ctx, time.Now()); err != nil {
		return err
	}
	if err := a.jobsRepo.ResetSendingJobs(ctx); err != nil {
		log.Printf("reset sending jobs: %v", err)
	}
	if err := a.dispatcher.DispatchDue(ctx, time.Now()); err != nil {
		log.Printf("initial dispatch skipped: %v", err)
	}

	go a.runLoop(ctx, "sheets-sync", a.settings.SyncInterval, func(loopCtx context.Context) error {
		return a.syncService.Sync(loopCtx)
	})
	go a.runLoop(ctx, "planner", a.settings.PlanningInterval, func(loopCtx context.Context) error {
		return a.planner.Plan(loopCtx, time.Now())
	})
	go a.runLoop(ctx, "dispatcher", a.settings.DispatchInterval, func(loopCtx context.Context) error {
		return a.dispatcher.DispatchDue(loopCtx, time.Now())
	})

	<-ctx.Done()
	return nil
}

func (a *App) Close() error {
	a.whatsApp.Close()
	if a.runtimeDB != nil {
		return a.runtimeDB.Close()
	}
	return nil
}

func (a *App) runLoop(ctx context.Context, name string, interval time.Duration, fn func(context.Context) error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := fn(ctx); err != nil {
				log.Printf("%s loop error: %v", name, err)
			}
		}
	}
}
