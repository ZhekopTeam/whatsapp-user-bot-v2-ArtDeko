package templates

import (
	"encoding/json"
	"fmt"
	"os"
)

type Catalog struct {
	Sentences []string `json:"sentences"`
}

func LoadCatalog(path string) (*Catalog, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read sentences file: %w", err)
	}

	var catalog Catalog
	if err := json.Unmarshal(content, &catalog); err != nil {
		return nil, fmt.Errorf("parse sentences file: %w", err)
	}
	if len(catalog.Sentences) < 2 {
		return nil, fmt.Errorf("sentences catalog must contain at least two phrases")
	}
	return &catalog, nil
}
