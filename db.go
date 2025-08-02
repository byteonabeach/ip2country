package ip2country

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// IPCountryDB implements the IPCountryLookup interface using a sorted list of IP ranges.
// It is optimized for lookups using binary search and is protected by a mutex for
// concurrent access.
type IPCountryDB struct {
	ranges      []IPRange
	mu          sync.RWMutex
	initialized int32
	initErr     error
	config      Config
	stats       Stats
	filePath    string
	cache       *lruCache
}

// NewIPCountryDB creates a new instance of IPCountryDB.
// The database is not loaded until the first lookup or an explicit call to Reload.
// It accepts an optional Config; if not provided, DefaultConfig() is used.
func NewIPCountryDB(filePath string, config ...Config) *IPCountryDB {
	cfg := DefaultConfig()
	if len(config) > 0 {
		cfg = config[0]
	}

	if cfg.Delimiter == "" {
		cfg.Delimiter = ","
	}
	if cfg.CacheSize <= 0 {
		cfg.CacheSize = 1000
	}

	return &IPCountryDB{
		filePath: filePath,
		config:   cfg,
		cache:    newLRUCache(cfg.CacheSize),
	}
}

// initializeWithContext handles the one-time loading and processing of the IP range data.
func (db *IPCountryDB) initializeWithContext(ctx context.Context) error {
	if atomic.LoadInt32(&db.initialized) == 1 {
		return db.initErr
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	if atomic.LoadInt32(&db.initialized) == 1 {
		return db.initErr
	}

	start := time.Now()
	result, err := db.parseFileWithContext(ctx, db.filePath)
	if err != nil {
		db.initErr = err
		return db.initErr
	}

	sort.Slice(result.Ranges, func(i, j int) bool {
		return result.Ranges[i].StartIP < result.Ranges[j].StartIP
	})

	if err := db.validateRanges(result.Ranges); err != nil {
		db.initErr = fmt.Errorf("range validation failed: %w", err)
		return db.initErr
	}

	db.ranges = result.Ranges
	db.stats = result.Stats
	db.stats.LoadTime = time.Since(start)
	db.stats.LastUpdate = time.Now()

	atomic.StoreInt32(&db.initialized, 1)
	return nil
}

// validateRanges checks for overlapping IP ranges in a sorted slice.
func (db *IPCountryDB) validateRanges(ranges []IPRange) error {
	for i := 0; i < len(ranges)-1; i++ {
		if ranges[i].EndIP >= ranges[i+1].StartIP {
			return fmt.Errorf("overlapping ranges detected: [%d-%d] and [%d-%d]",
				ranges[i].StartIP, ranges[i].EndIP, ranges[i+1].StartIP, ranges[i+1].EndIP)
		}
	}
	return nil
}

// parseFileWithContext opens and parses the data file.
func (db *IPCountryDB) parseFileWithContext(ctx context.Context, filePath string) (*ParseResult, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file stats: %w", err)
	}
	fileSize := stat.Size()
	if db.config.MaxFileSize > 0 && fileSize > db.config.MaxFileSize {
		return nil, fmt.Errorf("file size %d exceeds limit %d", fileSize, db.config.MaxFileSize)
	}

	result, err := db.parseReaderWithContext(ctx, file)
	if err != nil {
		return nil, err
	}

	result.Stats.FileSize = fileSize
	return result, nil
}

// parseReaderWithContext reads from an io.Reader and parses the data line by line.
func (db *IPCountryDB) parseReaderWithContext(ctx context.Context, reader io.Reader) (*ParseResult, error) {
	scanner := bufio.NewScanner(reader)
	var ranges []IPRange
	var errors []ParseError
	lineNum := 0

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || (db.config.SkipHeader && lineNum == 1) {
			continue
		}

		ipRange, err := db.parseLine(line)
		if err != nil {
			errors = append(errors, ParseError{Line: lineNum, Content: line, Err: err})
			continue
		}

		ranges = append(ranges, *ipRange)
		if db.config.MaxRanges > 0 && len(ranges) >= db.config.MaxRanges {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanner error: %w", err)
	}

	return &ParseResult{
		Ranges: ranges,
		Errors: errors,
		Stats:  Stats{TotalRanges: len(ranges)},
	}, nil
}

// parseLine parses a single line of text into an IPRange.
// Expected format: start_ip,end_ip,country_code
func (db *IPCountryDB) parseLine(line string) (*IPRange, error) {
	parts := strings.Split(line, db.config.Delimiter)
	if len(parts) != 3 {
		return nil, fmt.Errorf("incorrect number of fields: expected 3, got %d", len(parts))
	}

	startIP, err := parseIP(strings.TrimSpace(parts[0]))
	if err != nil {
		return nil, fmt.Errorf("invalid start IP %q: %w", parts[0], err)
	}
	endIP, err := parseIP(strings.TrimSpace(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("invalid end IP %q: %w", parts[1], err)
	}
	countryCode := strings.TrimSpace(parts[2])

	ipRange := &IPRange{
		StartIP: startIP,
		EndIP:   endIP,
		Country: countryCode, // Per new requirement, Country is the same as Code.
		Code:    countryCode,
	}

	if err := ipRange.Validate(); err != nil {
		return nil, err
	}
	return ipRange, nil
}

// findCountryForIP performs a binary search to find the country for a given IP number.
func (db *IPCountryDB) findCountryForIP(ipNum uint32) (string, string, error) {
	if entry, found := db.cache.get(ipNum); found {
		if !entry.found {
			return "", "", fmt.Errorf("country not found for IP (cached miss)")
		}
		return entry.country, entry.code, nil
	}

	idx := sort.Search(len(db.ranges), func(i int) bool {
		return db.ranges[i].StartIP > ipNum
	})

	var entry cacheEntry
	if idx > 0 {
		rangeItem := db.ranges[idx-1]
		if rangeItem.Contains(ipNum) {
			entry = cacheEntry{ip: ipNum, country: rangeItem.Country, code: rangeItem.Code, found: true}
			db.cache.put(ipNum, entry)
			return rangeItem.Country, rangeItem.Code, nil
		}
	}

	entry = cacheEntry{ip: ipNum, found: false}
	db.cache.put(ipNum, entry)
	return "", "", fmt.Errorf("country not found for IP")
}

// GetCountry retrieves the country code for a given IP address string.
func (db *IPCountryDB) GetCountry(ipStr string) (string, error) {
	return db.GetCountryWithContext(context.Background(), ipStr)
}

// GetCountryWithContext retrieves the country code, respecting the context.
func (db *IPCountryDB) GetCountryWithContext(ctx context.Context, ipStr string) (string, error) {
	if err := db.initializeWithContext(ctx); err != nil {
		return "", fmt.Errorf("initialization failed: %w", err)
	}

	ipNum, err := parseIP(ipStr)
	if err != nil {
		return "", fmt.Errorf("invalid IP: %w", err)
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	country, _, err := db.findCountryForIP(ipNum)
	return country, err
}

// GetCountryCode retrieves the country code for a given IP address string.
func (db *IPCountryDB) GetCountryCode(ipStr string) (string, error) {
	return db.GetCountryCodeWithContext(context.Background(), ipStr)
}

// GetCountryCodeWithContext retrieves the country code, respecting the context.
func (db *IPCountryDB) GetCountryCodeWithContext(ctx context.Context, ipStr string) (string, error) {
	if err := db.initializeWithContext(ctx); err != nil {
		return "", fmt.Errorf("initialization failed: %w", err)
	}

	ipNum, err := parseIP(ipStr)
	if err != nil {
		return "", fmt.Errorf("invalid IP: %w", err)
	}

	db.mu.RLock()
	defer db.mu.RUnlock()

	_, code, err := db.findCountryForIP(ipNum)
	return code, err
}

// Stats returns the current operational statistics of the database.
func (db *IPCountryDB) Stats() Stats {
	db.mu.RLock()
	s := db.stats
	db.mu.RUnlock()

	hits, misses := db.cache.getStats()
	s.CacheHits = hits
	s.CacheMisses = misses
	return s
}

// Reload clears the current dataset and loads it again from the source file.
func (db *IPCountryDB) Reload() error {
	return db.ReloadWithContext(context.Background())
}

// ReloadWithContext reloads the dataset, respecting the context for cancellation.
func (db *IPCountryDB) ReloadWithContext(ctx context.Context) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	atomic.StoreInt32(&db.initialized, 0)
	db.ranges = nil
	db.initErr = nil
	db.cache.clear()

	err := db.initializeWithContext(ctx)
	if err != nil {
		return fmt.Errorf("reload failed: %w", err)
	}
	return nil
}
