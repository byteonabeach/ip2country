package ip2country

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ExactIPCountryMap implements the IPCountryLookup interface using a map for exact IP matches.
// This is suitable for datasets where specific IPs are mapped to countries, rather than ranges.
// It expects a CSV format of: ip,country_code
type ExactIPCountryMap struct {
	ipMap       map[uint32]string
	mu          sync.RWMutex
	initialized int32
	initErr     error
	config      Config
	stats       Stats
	filePath    string
	cache       *lruCache
	parseErrors []ParseError
}

// NewExactIPCountryMap creates a new instance of ExactIPCountryMap.
// The data is not loaded until the first lookup or an explicit call to Reload.
func NewExactIPCountryMap(filePath string, config ...Config) *ExactIPCountryMap {
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

	return &ExactIPCountryMap{
		filePath: filePath,
		config:   cfg,
		cache:    newLRUCache(cfg.CacheSize),
	}
}

// initializeWithContext handles the one-time loading of the IP map from a file.
func (m *ExactIPCountryMap) initializeWithContext(ctx context.Context) error {
	if atomic.LoadInt32(&m.initialized) == 1 {
		return m.initErr
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if atomic.LoadInt32(&m.initialized) == 1 {
		return m.initErr
	}

	start := time.Now()
	err := m.parseFileWithContext(ctx, m.filePath)
	if err != nil {
		m.initErr = err
		return m.initErr
	}

	m.stats.LoadTime = time.Since(start)
	m.stats.LastUpdate = time.Now()
	m.stats.TotalRanges = len(m.ipMap)

	atomic.StoreInt32(&m.initialized, 1)
	return nil
}

func (m *ExactIPCountryMap) parseFileWithContext(ctx context.Context, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to get file stats: %w", err)
	}
	fileSize := stat.Size()
	if m.config.MaxFileSize > 0 && fileSize > m.config.MaxFileSize {
		return fmt.Errorf("file size %d exceeds limit %d", fileSize, m.config.MaxFileSize)
	}

	m.ipMap = make(map[uint32]string)
	m.parseErrors = nil

	scanner := bufio.NewScanner(file)
	lineNum, processed := 0, 0

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || (m.config.SkipHeader && lineNum == 1) {
			continue
		}

		code, ipNum, err := m.parseLine(line)
		if err != nil {
			m.parseErrors = append(m.parseErrors, ParseError{Line: lineNum, Content: line, Err: err})
			continue
		}

		m.ipMap[ipNum] = code

		processed++
		if m.config.MaxRanges > 0 && processed >= m.config.MaxRanges {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scanner error: %w", err)
	}

	m.stats.FileSize = fileSize
	return nil
}

// parseLine parses a single line for the exact IP map.
// Expected format: ip,country_code
func (m *ExactIPCountryMap) parseLine(line string) (code string, ipNum uint32, err error) {
	parts := strings.Split(line, m.config.Delimiter)
	if len(parts) != 2 {
		err = fmt.Errorf("incorrect number of fields: expected 2, got %d", len(parts))
		return
	}

	ipStr := strings.TrimSpace(parts[0])
	ipNum, err = parseIP(ipStr)
	if err != nil {
		err = fmt.Errorf("invalid IP %q: %w", ipStr, err)
		return
	}

	code = strings.TrimSpace(parts[1])
	if code == "" {
		err = fmt.Errorf("country code cannot be empty")
		return
	}

	return
}

// GetParseErrors returns any errors that occurred during the last load/reload.
func (m *ExactIPCountryMap) GetParseErrors() []ParseError {
	m.mu.RLock()
	defer m.mu.RUnlock()
	errorsCopy := make([]ParseError, len(m.parseErrors))
	copy(errorsCopy, m.parseErrors)
	return errorsCopy
}

// findCountryForIP looks up an IP in the map, using the cache.
func (m *ExactIPCountryMap) findCountryForIP(ipNum uint32) (string, string, error) {
	if entry, found := m.cache.get(ipNum); found {
		if !entry.found {
			return "", "", fmt.Errorf("country not found for IP (cached miss)")
		}
		return entry.country, entry.code, nil
	}

	code, countryExists := m.ipMap[ipNum]
	if !countryExists {
		m.cache.put(ipNum, cacheEntry{ip: ipNum, found: false})
		return "", "", fmt.Errorf("country not found for IP")
	}

	m.cache.put(ipNum, cacheEntry{ip: ipNum, country: code, code: code, found: true})
	return code, code, nil
}

// GetCountry retrieves the country code for a given IP address string.
func (m *ExactIPCountryMap) GetCountry(ipStr string) (string, error) {
	return m.GetCountryWithContext(context.Background(), ipStr)
}

// GetCountryWithContext retrieves the country code, respecting the context.
func (m *ExactIPCountryMap) GetCountryWithContext(ctx context.Context, ipStr string) (string, error) {
	if err := m.initializeWithContext(ctx); err != nil {
		return "", fmt.Errorf("initialization failed: %w", err)
	}

	ipNum, err := parseIP(ipStr)
	if err != nil {
		return "", fmt.Errorf("invalid IP: %w", err)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	country, _, err := m.findCountryForIP(ipNum)
	return country, err
}

// GetCountryCode retrieves the country code for a given IP address string.
func (m *ExactIPCountryMap) GetCountryCode(ipStr string) (string, error) {
	return m.GetCountryCodeWithContext(context.Background(), ipStr)
}

// GetCountryCodeWithContext retrieves the country code, respecting the context.
func (m *ExactIPCountryMap) GetCountryCodeWithContext(ctx context.Context, ipStr string) (string, error) {
	if err := m.initializeWithContext(ctx); err != nil {
		return "", fmt.Errorf("initialization failed: %w", err)
	}

	ipNum, err := parseIP(ipStr)
	if err != nil {
		return "", fmt.Errorf("invalid IP: %w", err)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	_, code, err := m.findCountryForIP(ipNum)
	return code, err
}

// Stats returns the current operational statistics of the map.
func (m *ExactIPCountryMap) Stats() Stats {
	m.mu.RLock()
	s := m.stats
	m.mu.RUnlock()

	hits, misses := m.cache.getStats()
	s.CacheHits = hits
	s.CacheMisses = misses
	return s
}

// Reload clears the current dataset and loads it again from the source file.
func (m *ExactIPCountryMap) Reload() error {
	return m.ReloadWithContext(context.Background())
}

// ReloadWithContext reloads the dataset, respecting the context for cancellation.
func (m *ExactIPCountryMap) ReloadWithContext(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	atomic.StoreInt32(&m.initialized, 0)
	m.ipMap = nil
	m.initErr = nil
	m.cache.clear()

	err := m.initializeWithContext(ctx)
	if err != nil {
		return fmt.Errorf("reload failed: %w", err)
	}
	return nil
}
