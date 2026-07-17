package sheets

import (
	"context"
	"fmt"

	"my-whatsapp-bot/wa-user-bot/storage/sqlite"
)

const syncSourceName = "google_sheets"

type SyncService struct {
	client              *Client
	accountsSheetName   string
	communicationsSheet string
	accountsRepo        *sqlite.AccountsRepo
	communicationsRepo  *sqlite.CommunicationsRepo
	syncStateRepo       *sqlite.SyncStateRepo
}

func NewSyncService(
	client *Client,
	accountsSheetName string,
	communicationsSheet string,
	accountsRepo *sqlite.AccountsRepo,
	communicationsRepo *sqlite.CommunicationsRepo,
	syncStateRepo *sqlite.SyncStateRepo,
) *SyncService {
	return &SyncService{
		client:              client,
		accountsSheetName:   accountsSheetName,
		communicationsSheet: communicationsSheet,
		accountsRepo:        accountsRepo,
		communicationsRepo:  communicationsRepo,
		syncStateRepo:       syncStateRepo,
	}
}

func (s *SyncService) Sync(ctx context.Context) error {
	accountRows, err := s.client.ReadRange(ctx, s.accountsSheetName+"!A:B")
	if err != nil {
		_ = s.syncStateRepo.MarkFailure(ctx, syncSourceName, err.Error())
		return err
	}
	accounts, err := MapAccounts(accountRows)
	if err != nil {
		_ = s.syncStateRepo.MarkFailure(ctx, syncSourceName, err.Error())
		return err
	}
	if err := s.accountsRepo.UpsertMany(ctx, accounts); err != nil {
		_ = s.syncStateRepo.MarkFailure(ctx, syncSourceName, err.Error())
		return fmt.Errorf("sync accounts: %w", err)
	}

	if err := s.syncStateRepo.MarkSuccess(ctx, syncSourceName); err != nil {
		return err
	}

	return nil
}
