package wallet

import (
	"strings"
	"time"
)

type Source string

const (
	SourceCurated    Source = "curated"
	SourceDiscovered Source = "discovered"
	SourceBlocked    Source = "blocked"
)

type Record struct {
	Address     string    `json:"address" yaml:"address"`
	Label       string    `json:"label" yaml:"label"`
	Source      Source    `json:"source" yaml:"source"`
	Tags        []string  `json:"tags" yaml:"tags"`
	Collections []string  `json:"collections" yaml:"collections"`
	Evidence    []string  `json:"evidence" yaml:"evidence"`
	Score       float64   `json:"score" yaml:"score"`
	Active      bool      `json:"active" yaml:"active"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func NormalizeAddress(addr string) string {
	return strings.ToLower(strings.TrimSpace(addr))
}

type SeedFile struct {
	Wallets []Record `yaml:"wallets"`
}
