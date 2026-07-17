package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"my-whatsapp-bot/wa-user-bot/api"
	"my-whatsapp-bot/wa-user-bot/config"
	"my-whatsapp-bot/wa-user-bot/domain"
	"my-whatsapp-bot/wa-user-bot/scheduler"
	"my-whatsapp-bot/wa-user-bot/sheets"
	"my-whatsapp-bot/wa-user-bot/storage/sqlite"
	"my-whatsapp-bot/wa-user-bot/templates"
	"my-whatsapp-bot/wa-user-bot/whatsapp"
)

type App struct {
	settings      *config.Settings
	runtimeDB     *sql.DB
	whatsApp      *whatsapp.Manager
	syncService   *sheets.SyncService
	planner       *scheduler.Planner
	dispatcher    *scheduler.Dispatcher
	accountsRepo  *sqlite.AccountsRepo
	sessionsRepo  *sqlite.SessionsRepo
	jobsRepo      *sqlite.JobsRepo
	apiServer     *api.Server
	adminProxyRepo *sqlite.AdminProxyRepo
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

	// Wire proxy lookup from admin DB (non-fatal if DB is unavailable)
	adminProxyRepo, proxyErr := sqlite.NewAdminProxyRepo(settings.AdminDBPath)
	if proxyErr != nil {
		log.Printf("admin proxy db unavailable, running without proxy support: %v", proxyErr)
	}
	if adminProxyRepo != nil {
		manager.ProxyLookup = func(ctx context.Context, phone string) (*domain.Proxy, error) {
			return adminProxyRepo.GetProxyForPhone(ctx, phone)
		}
	}

	sender := whatsapp.NewSender(manager)
	apiServer := api.NewServer(manager, settings.APIPort)
	dispatcher := scheduler.NewDispatcher(
		jobsRepo,
		accountsRepo,
		adminProxyRepo,
		sender,
		generator,
		settings.DispatchBatchSize,
		location,
		22,
		12, // match admin group warmup REPLY_DELAY_MIN/MAX
		20,
	)

	// Status updates in runtime DB + mirror session loss to admin Telegram bot DB.
	manager.OnStatusChange = func(phone, status string) {
		if err := accountsRepo.UpdateStatusByPhone(context.Background(), phone, status); err != nil {
			log.Printf("update account status for phone %s: %v", phone, err)
		}
		if adminProxyRepo != nil {
			switch status {
			case domain.AccountStatusBlocked, domain.AccountStatusFailed:
				if err := adminProxyRepo.SetAccountStatusByPhone(
					context.Background(), phone, "revoked",
				); err != nil {
					if !errors.Is(err, sql.ErrNoRows) && !strings.Contains(err.Error(), "no rows") {
						log.Printf("mark admin account %s as revoked: %v", phone, err)
					}
				} else {
					log.Printf("admin account %s marked revoked (session lost)", phone)
				}
			case domain.AccountStatusReady:
				if err := adminProxyRepo.SetAccountStatusByPhone(
					context.Background(), phone, "active",
				); err != nil {
					if !errors.Is(err, sql.ErrNoRows) && !strings.Contains(err.Error(), "no rows") {
						log.Printf("restore admin account %s as active: %v", phone, err)
					}
				}
			}
		}
		if status == domain.AccountStatusBlocked || status == domain.AccountStatusFailed {
			dispatcher.ExcludeAccountByPhone(
				context.Background(),
				phone,
				fmt.Sprintf("cancelled: account status %s", status),
			)
		}
	}

	return &App{
		settings:       settings,
		runtimeDB:      runtimeDB,
		whatsApp:       manager,
		accountsRepo:   accountsRepo,
		sessionsRepo:   sessionsRepo,
		jobsRepo:       jobsRepo,
		apiServer:      apiServer,
		adminProxyRepo: adminProxyRepo,
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
		dispatcher: dispatcher,
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

	go func() {
		if err := a.apiServer.Start(ctx); err != nil {
			log.Printf("api server error: %v", err)
		}
	}()

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
	if a.adminProxyRepo != nil {
		a.adminProxyRepo.Close()
	}
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
