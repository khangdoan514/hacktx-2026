package catalogmcp

// Search is the read-only fictional vendor catalog. "price" and "cost" do
// not filter by themselves; "accessibility" keeps accessible vendors; any
// other criterion must match a feature name. A positive budget excludes
// vendors above it.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Vendor struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	MonthlyUSD int      `json:"monthly_usd"`
	Accessible bool     `json:"accessible"`
	Features   []string `json:"features"`
	Notes      string   `json:"notes"`
}

type Catalog struct {
	vendors []Vendor
}

type Query struct {
	Criteria  []string `json:"criteria"`
	BudgetUSD float64  `json:"budget_usd"`
}

type Result struct {
	Vendors   []Vendor `json:"vendors"`
	SourceIDs []string `json:"source_ids"`
}

func Load(path string) (*Catalog, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read catalog %s: %w", path, err)
	}
	var vendors []Vendor
	if err := json.Unmarshal(data, &vendors); err != nil {
		return nil, fmt.Errorf("parse catalog %s: %w", path, err)
	}
	if len(vendors) == 0 {
		return nil, fmt.Errorf("empty catalog %s", path)
	}
	return &Catalog{vendors: vendors}, nil
}

func (c *Catalog) Search(ctx context.Context, query Query) (Result, error) {
	result := Result{Vendors: []Vendor{}, SourceIDs: []string{}}
	for _, vendor := range c.vendors {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if query.BudgetUSD > 0 && float64(vendor.MonthlyUSD) > query.BudgetUSD {
			continue
		}
		if !matches(vendor, query.Criteria) {
			continue
		}
		result.Vendors = append(result.Vendors, vendor)
		result.SourceIDs = append(result.SourceIDs, vendor.ID)
	}
	return result, nil
}

func matches(vendor Vendor, criteria []string) bool {
	for _, criterion := range criteria {
		key := strings.ToLower(strings.TrimSpace(criterion))
		switch key {
		case "accessibility", "accessible":
			if !vendor.Accessible {
				return false
			}
		case "price", "cost", "":
			continue
		default:
			found := false
			for _, feature := range vendor.Features {
				if strings.EqualFold(feature, key) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}
