package tools

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/khangdoan514/hacktx-2026/agents/internal/contracts"
)

//go:embed data/vendors.json
var vendorFixture []byte

type Vendor struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	MonthlyUSD int      `json:"monthly_usd"`
	Accessible bool     `json:"accessible"`
	Features   []string `json:"features"`
	Notes      string   `json:"notes"`
}

type Catalog struct{ vendors []Vendor }

var _ contracts.Tool = (*Catalog)(nil)

func NewCatalog() (*Catalog, error) {
	var vendors []Vendor
	if err := json.Unmarshal(vendorFixture, &vendors); err != nil {
		return nil, fmt.Errorf("invalid catalog fixture: %w", err)
	}
	if len(vendors) == 0 {
		return nil, fmt.Errorf("empty catalog fixture")
	}
	return &Catalog{vendors: vendors}, nil
}

func (c *Catalog) Definition() contracts.ToolDefinition {
	return contracts.ToolDefinition{
		ID: "catalog.search", Version: "1.0.0", Description: "Search a checked-in fictional vendor catalog", SideEffect: "READ_ONLY", TimeoutSeconds: 5,
		InputSchema:  json.RawMessage(`{"type":"object","properties":{"criteria":{"type":"array","items":{"type":"string"}},"budget_usd":{"type":"number"}},"additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object","properties":{"vendors":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"name":{"type":"string"},"monthly_usd":{"type":"integer"},"accessible":{"type":"boolean"},"features":{"type":"array","items":{"type":"string"}},"notes":{"type":"string"}},"required":["id","name","monthly_usd","accessible","features","notes"],"additionalProperties":false}},"source_ids":{"type":"array","items":{"type":"string"}}},"required":["vendors","source_ids"],"additionalProperties":false}`),
	}
}

func (c *Catalog) Invoke(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var query struct {
		Criteria  []string `json:"criteria"`
		BudgetUSD float64  `json:"budget_usd"`
	}
	if err := json.Unmarshal(input, &query); err != nil {
		return nil, &contracts.RunError{Code: contracts.ErrInvalidInput, Message: "invalid catalog query"}
	}
	result := struct {
		Vendors   []Vendor `json:"vendors"`
		SourceIDs []string `json:"source_ids"`
	}{Vendors: make([]Vendor, 0), SourceIDs: make([]string, 0)}
	for _, vendor := range c.vendors {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if query.BudgetUSD > 0 && float64(vendor.MonthlyUSD) > query.BudgetUSD {
			continue
		}
		matches := true
		for _, criterion := range query.Criteria {
			key := strings.ToLower(strings.TrimSpace(criterion))
			if key == "accessibility" || key == "accessible" {
				if !vendor.Accessible {
					matches = false
				}
				continue
			}
			if key == "price" || key == "cost" {
				continue
			}
			found := false
			for _, feature := range vendor.Features {
				if strings.EqualFold(feature, key) {
					found = true
					break
				}
			}
			if !found {
				matches = false
			}
		}
		if matches {
			result.Vendors = append(result.Vendors, vendor)
			result.SourceIDs = append(result.SourceIDs, vendor.ID)
		}
	}
	return json.Marshal(result)
}
