package app

import (
	"context"
	"fmt"
	"time"

	"my-whatsapp-bot/internal/domain"
)

func (a *App) AuthAccount(ctx context.Context, accountID int64) error {
	if err := a.syncService.Sync(ctx); err != nil {
		return fmt.Errorf("sync before auth: %w", err)
	}

	account, err := a.accountsRepo.GetByID(ctx, accountID)
	if err != nil {
		return fmt.Errorf("load account %d: %w", accountID, err)
	}
	if account.Phone == "" {
		return fmt.Errorf("account %d has empty phone", accountID)
	}

	deviceJID, alreadyExists, err := a.whatsApp.EnsureSession(ctx, account.Phone)
	if err != nil {
		_ = a.accountsRepo.UpdateStatus(ctx, accountID, domain.AccountStatusFailed)
		_ = a.sessionsRepo.Upsert(ctx, domain.SessionState{
			AccountID:    accountID,
			DeviceJID:    "",
			IsAuthorized: false,
			IsConnected:  false,
			LastError:    err.Error(),
			UpdatedAt:    time.Now().UTC(),
		})
		return err
	}

	now := time.Now().UTC()
	if err := a.accountsRepo.UpdateStatus(ctx, accountID, domain.AccountStatusReady); err != nil {
		return err
	}
	if err := a.sessionsRepo.Upsert(ctx, domain.SessionState{
		AccountID:       accountID,
		DeviceJID:       deviceJID,
		IsAuthorized:    true,
		IsConnected:     alreadyExists,
		LastConnectedAt: &now,
		LastError:       "",
		UpdatedAt:       now,
	}); err != nil {
		return err
	}

	return nil
}
