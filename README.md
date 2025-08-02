# ip2country

[Go Reference](https://pkg.go.dev/github.com/byteonabeach/ip2country)
[![Go Report Card](https://goreportcard.com/badge/github.com/byteonabeach/ip2country)](https://goreportcard.com/report/github.com/byteonabeach/ip2country)

[🇷🇺 Read in Russian](./README_RU.md)

A fast and efficient Go library for IP to country lookup.

This package is designed to work with the free IP-to-Country CSV database from **[DB-IP](https://db-ip.com/db/format/ip-to-country/csv.html)**. It parses the `start_ip,end_ip,country_code` format and provides a fast, thread-safe interface for lookups.

### Features

-   **High Performance**: Uses binary search on a sorted range list for quick lookups (`IPCountryDB`).
-   **Two Strategies**:
    -   `IPCountryDB`: Ideal for large, contiguous IP range datasets.
    -   `ExactIPCountryMap`: Optimized for specific, non-contiguous IP-to-country mappings.
-   **Thread-Safe**: Designed for concurrent use in high-load services.
-   **LRU Cache**: Built-in cache to dramatically speed up repeated lookups for the same IPs.
-   **On-Demand Reloading**: The database can be reloaded at runtime without service interruption.
-   **Zero Dependencies**: Relies only on the Go standard library.

### Installation

```sh
go get -u github.com/byteonabeach/ip2country
```
### Example Usage

Here is a complete example of how to use `IPCountryDB`.

**1. Prepare your data file (`ip_to_country.csv`)**

Download the CSV from [DB-IP](https://db-ip.com/db/format/ip-to-country/csv.html) or create a file with the following format:

```csv
1.0.0.0,1.0.0.255,AU
1.0.1.0,1.0.3.255,CN
1.0.4.0,1.0.7.255,AU
1.0.8.0,1.0.15.255,CN
8.8.8.0,8.8.8.255,US
```

**2. Write your test Go script**

```go
// main.go
package main

import (
	"fmt"
	"log"

	"github.com/byteonabeach/ip2country"
)

func main() {
	// Initialize the database with the path to your CSV file.
	// The data is loaded on the first lookup.
	db := ip2country.NewIPCountryDB("ip_to_country.csv")

	// --- Test IPs ---
	ipsToTest := []string{
		"1.0.1.15",    // Should be CN
		"8.8.8.8",     // Should be US
		"1.0.5.100",   // Should be AU
		"127.0.0.1",   // Should be ZZ (special range)
	}

	fmt.Println("--- Looking up country codes ---")
	for _, ip := range ipsToTest {
		// GetCountryCode is the primary method for lookup.
		code, err := db.GetCountryCode(ip)
		if err != nil {
			log.Printf("Error looking up %s: %v", ip, err)
		} else {
			fmt.Printf("Country Code for %s is: %s\n", ip, code)
		}
	}

	// You can also get stats about the loaded data
	stats := db.Stats()
	fmt.Printf("\n--- DB Stats ---\n")
	fmt.Printf("Total Ranges: %d\n", stats.TotalRanges)
	fmt.Printf("Load Time: %s\n", stats.LoadTime)
	fmt.Printf("Cache Hits: %d, Cache Misses: %d\n", stats.CacheHits, stats.CacheMisses)
}
```

**3. Run the code**

```sh
go run main.go
```

**Expected Output:**

```
--- Looking up country codes ---
Country Code for 1.0.1.15 is: CN
Country Code for 8.8.8.8 is: US
Country Code for 1.0.5.100 is: AU
Country Code for 127.0.0.1 is: ZZ

--- DB Stats ---
Total Ranges: 342632
Load Time: 148.7ms
Cache Hits: 0, Cache Misses: 4
```

### To-Do / Future Plans
-   [ ] **IPv6 Support**: Add the ability to parse and look up IPv6 ranges.
-   [ ] **More Data Sources**: Add parsers for other popular formats (e.g., MaxMind GeoLite2).
-   [ ] **Benchmarks**: Implement a comprehensive set of benchmarks to track performance.
-   [ ] **CLI Tool**: Create a simple command-line utility for quick lookups from the terminal.
-   [ ] **More Config Options**: Add more flexible configuration, for example, for the LRU cache behavior.
