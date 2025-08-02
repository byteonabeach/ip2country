// Package ip2country provides fast and efficient IP to country lookup.
// It supports two lookup methods:
//
//  1. IPCountryDB: A range-based lookup database, efficient for large datasets
//     representing the entire IP address space. It loads a CSV file and uses a
//     binary search for lookups.
//
//  2. ExactIPCountryMap: An exact-match lookup map, suitable for smaller datasets
//     where each IP address is mapped directly to a country code.
//
// The recommended data source for IPCountryDB is the free IP-to-Country CSV database
// from DB-IP: https://db-ip.com/db/format/ip-to-country/csv.html
// This package is designed to parse its specific format: start_ip,end_ip,country_code
//
// Both implementations feature thread-safe operations, an in-memory LRU cache to
// speed up repeated lookups, and on-demand reloading of the dataset.
package ip2country

import (
	"context"
	"fmt"
	"os"
	"sort"
	"time"
)

// IPCountryLookup defines the interface for IP to country lookup services.
// It provides methods to get country information from an IP address string.
type IPCountryLookup interface {
	// GetCountry retrieves the country code for a given IP address string.
	// In the current implementation, this returns the same value as GetCountryCode.
	GetCountry(ipStr string) (string, error)
	// GetCountryCode retrieves the country code (e.g., "US") for a given IP address string.
	GetCountryCode(ipStr string) (string, error)
	// GetCountryWithContext retrieves the country code, respecting the context.
	GetCountryWithContext(ctx context.Context, ipStr string) (string, error)
	// GetCountryCodeWithContext retrieves the country code, respecting the context.
	GetCountryCodeWithContext(ctx context.Context, ipStr string) (string, error)
	// Stats returns the current operational statistics of the database.
	Stats() Stats
	// Reload clears the current dataset and loads it again from the source file.
	Reload() error
	// ReloadWithContext reloads the dataset, respecting the context for cancellation.
	ReloadWithContext(ctx context.Context) error
}

// Config holds configuration parameters for the IP lookup databases.
// Fields are ordered for optimal memory alignment.
type Config struct {
	// Delimiter specifies the character used to separate fields in the CSV file.
	Delimiter string
	// MaxFileSize limits the size of the file to be loaded, preventing excessive memory usage.
	// The value is in bytes. A value of 0 or less means no limit.
	MaxFileSize int64
	// MaxRanges sets the maximum number of IP ranges or entries to load from the file.
	// A value of 0 or less means no limit.
	MaxRanges int
	// CacheSize defines the number of entries to keep in the LRU cache.
	// If set to 0 or less, a default value will be used.
	CacheSize int
	// SkipHeader indicates whether the first line of the CSV file should be skipped.
	SkipHeader bool
}

// DefaultConfig returns a new Config with sensible default values.
func DefaultConfig() Config {
	return Config{
		MaxRanges:   1000000,
		MaxFileSize: 100 << 20, // 100 MB
		SkipHeader:  false,
		Delimiter:   ",",
		CacheSize:   1000,
	}
}

// Stats provides operational statistics for an IP lookup database.
// Fields are ordered for optimal memory alignment.
type Stats struct {
	// LastUpdate is the timestamp of the last successful data load or reload.
	LastUpdate time.Time `json:"last_update"`
	// LoadTime is the duration it took to load the dataset.
	LoadTime time.Duration `json:"load_time"`
	// FileSize is the size of the source data file in bytes.
	FileSize int64 `json:"file_size"`
	// CacheHits is the number of times a lookup was served from the cache.
	CacheHits int64 `json:"cache_hits"`
	// CacheMisses is the number of times a lookup was not found in the cache.
	CacheMisses int64 `json:"cache_misses"`
	// TotalRanges is the number of IP ranges or entries currently loaded.
	TotalRanges int `json:"total_ranges"`
}

// IPRange represents a continuous range of IP addresses belonging to a single country.
// Fields are ordered for optimal memory alignment.
type IPRange struct {
	// Country is the country code (e.g., US, DE).
	Country string `json:"country"`
	// Code is the two-letter country code.
	Code string `json:"code"`
	// StartIP is the starting IP address of the range, as a 32-bit unsigned integer.
	StartIP uint32 `json:"start_ip"`
	// EndIP is the ending IP address of the range, as a 32-bit unsigned integer.
	EndIP uint32 `json:"end_ip"`
}

// Contains checks if a given IP address (as a uint32) is within the range.
func (r IPRange) Contains(ip uint32) bool {
	return ip >= r.StartIP && ip <= r.EndIP
}

// Validate checks if the IPRange is valid.
// A range is valid if the start IP is not greater than the end IP and the code is not empty.
func (r IPRange) Validate() error {
	if r.StartIP > r.EndIP {
		return fmt.Errorf("invalid range: start IP %d > end IP %d", r.StartIP, r.EndIP)
	}
	if r.Code == "" {
		return fmt.Errorf("country code cannot be empty")
	}
	return nil
}

// ParseError represents an error that occurred while parsing a line from the data file.
// Fields are ordered for optimal memory alignment.
type ParseError struct {
	// Content is the actual content of the line that caused the error.
	Content string
	// Err is the underlying error.
	Err error
	// Line is the line number where the error occurred.
	Line int
}

// Error returns a string representation of the ParseError.
func (e ParseError) Error() string {
	return fmt.Sprintf("line %d: %v (content: %q)", e.Line, e.Err, e.Content)
}

// ParseResult holds the outcome of a file parsing operation.
type ParseResult struct {
	// Ranges is the slice of successfully parsed IP ranges.
	Ranges []IPRange
	// Errors is a slice of errors encountered during parsing.
	Errors []ParseError
	// Stats contains statistics about the parsing process.
	Stats Stats
}

// ValidateIPRanges checks a slice of IPRange for validity and overlaps.
// It sorts the ranges by StartIP and then ensures that no two ranges overlap
// and that each individual range is valid.
func ValidateIPRanges(ranges []IPRange) error {
	if len(ranges) == 0 {
		return nil
	}

	sorted := make([]IPRange, len(ranges))
	copy(sorted, ranges)

	// Sort by start IP to easily check for overlaps.
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartIP < sorted[j].StartIP
	})

	// Validate the first range.
	if err := sorted[0].Validate(); err != nil {
		return fmt.Errorf("invalid range at index 0 (after sorting): %w", err)
	}

	for i := 1; i < len(sorted); i++ {
		current := sorted[i]
		previous := sorted[i-1]

		if err := current.Validate(); err != nil {
			return fmt.Errorf("invalid range at index %d (after sorting): %w", i, err)
		}

		if previous.EndIP >= current.StartIP {
			return fmt.Errorf("overlapping ranges: [%d-%d] and [%d-%d]",
				previous.StartIP, previous.EndIP, current.StartIP, current.EndIP)
		}
	}

	return nil
}

// ParseCSVRanges is a utility function that parses a CSV file containing IP ranges
// without creating a full DB instance. It's useful for pre-validating or inspecting data.
func ParseCSVRanges(filePath string, config ...Config) (*ParseResult, error) {
	cfg := DefaultConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	if cfg.MaxFileSize > 0 {
		stat, err := os.Stat(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to get file stats: %w", err)
		}
		if stat.Size() > cfg.MaxFileSize {
			return nil, fmt.Errorf("file size %d exceeds limit %d", stat.Size(), cfg.MaxFileSize)
		}
	}

	db := &IPCountryDB{
		filePath: filePath,
		config:   cfg,
	}
	return db.parseFileWithContext(context.Background(), filePath)
}
