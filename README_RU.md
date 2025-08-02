# ip2country

[![Go Report Card](https://goreportcard.com/badge/github.com/byteonabeach/ip2country)](https://goreportcard.com/report/github.com/byteonabeach/ip2country)
[Go Reference](https://pkg.go.dev/github.com/byteonabeach/ip2country)

Быстрая и эффективная Go-библиотека для определения страны по IP-адресу.
Эта либа предназначена для работы с базой данных IP-адресов **[DB-IP](https://db-ip.com/db/format/ip-to-country/csv.html)**. 
Она парсит CSV-файлы формата `start_ip,end_ip,country_code` и предоставляет быстрый, потокобезопасный интерфейс для поиска.

### Возможности

-   **Высокая производительность**: Использует бинарный поиск по отсортированному списку диапазонов для быстрого поиска (`IPCountryDB`).
-   **Вариативность использования**:
    -   `IPCountryDB`: подходит для больших наборов данных с непрерывными диапазонами IP.
    -   `ExactIPCountryMap`: для точных сопоставлений "IP-страна", когда диапазоны не используются.
-   **Concurrent safety**: Разработано для конкурентного использования в высоконагруженных сервисах.
-   **Кэш LRU**: кэш для значительного ускорения повторных запросов для одних и тех же IP.
-   **Перезагрузка на лету**: БД может быть загружена из другого источника во время работы сервиса без его остановки.
-   **Zero dependencies**: Пакет использует только стандартную библиотеку Go.

### Установка

```sh
go get -u github.com/byteonabeach/ip2country
```

### Пример использования

Ниже приведен пример использования `IPCountryDB`.

**1. Подготовьте БД (`ip_to_country.csv`)**

Загрузите CSV с сайта [DB-IP](https://db-ip.com/db/format/ip-to-country/csv.html) или создайте файл в следующем формате:

```csv
1.0.0.0,1.0.0.255,AU
1.0.1.0,1.0.3.255,CN
1.0.4.0,1.0.7.255,AU
1.0.8.0,1.0.15.255,CN
8.8.8.0,8.8.8.255,US
```

**2. Напишите тестовый скрипт**

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

**3. Запустите код**

```sh
go run main.go
```

**Ожидаемый вывод:**
```
--- Looking up country codes ---
Country Code for 1.0.1.15 is: CN
Country Code for 8.8.8.8 is: US
Country Code for 1.0.5.100 is: AU
Country Code for 127.0.0.1 is: ZZ

--- DB Stats ---
Total Ranges: 342632
Load Time: 148.7sms
Cache Hits: 0, Cache Misses: 4
```

### To-Do  
-   [ ] **Поддержка IPv6**: Добавить возможность парсить и искать диапазоны IPv6.
-   [ ] **Больше источников данных**: Реализовать парсеры для других популярных форматов (например, MaxMind GeoLite2).
-   [ ] **Тесты производительности**: Добавить подробный набор бенчмарков для отслеживания производительности.
-   [ ] **CLI-утилита**: Создать простую утилиту командной строки для быстрого поиска из терминала.
-   [ ] **Расширение конфигурации**: Добавить больше гибких настроек, например, для управления поведением LRU-кэша.
