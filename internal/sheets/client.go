package sheets

import (
	"context"
	"fmt"

	"google.golang.org/api/option"
	googleSheets "google.golang.org/api/sheets/v4"
)

type Client struct {
	service *googleSheets.Service
	fileID  string
}

func NewClient(ctx context.Context, credentialsPath string, spreadsheetID string) (*Client, error) {
	service, err := googleSheets.NewService(
		ctx,
		option.WithCredentialsFile(credentialsPath),
		option.WithScopes(googleSheets.SpreadsheetsReadonlyScope),
	)
	if err != nil {
		return nil, fmt.Errorf("initialize google sheets client: %w", err)
	}

	return &Client{service: service, fileID: spreadsheetID}, nil
}

func (c *Client) ReadRange(ctx context.Context, rangeRef string) ([][]interface{}, error) {
	response, err := c.service.Spreadsheets.Values.Get(c.fileID, rangeRef).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("read range %s: %w", rangeRef, err)
	}
	return response.Values, nil
}
