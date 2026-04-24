// Package main provides the entry point for the awesome-go tool.
// It validates links in the README.md and checks for formatting issues.
package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	// readmeFile is the path to the README file to validate
	readmeFile = "README.md"
	// httpTimeout is the timeout for HTTP requests when checking links
	httpTimeout = 15 * time.Second
	// maxConcurrent is the maximum number of concurrent link checks
	maxConcurrent = 10
)

// LinkResult holds the result of a link check
type LinkResult struct {
	URL        string
	StatusCode int
	Err        error
}

// extractLinks parses a markdown file and returns all URLs found
func extractLinks(filename string) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Match markdown links: [text](url)
	linkRegex := regexp.MustCompile(`\[.*?\]\((https?://[^)]+)\)`)

	var links []string
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		matches := linkRegex.FindAllStringSubmatch(line, -1)
		for _, match := range matches {
			if len(match) > 1 {
				url := strings.TrimSpace(match[1])
				if !seen[url] {
					seen[url] = true
					links = append(links, url)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error scanning file: %w", err)
	}

	return links, nil
}

// checkLink performs an HTTP HEAD request to verify a URL is accessible
func checkLink(client *http.Client, url string) LinkResult {
	resp, err := client.Head(url)
	if err != nil {
		// Fallback to GET if HEAD is not supported
		resp, err = client.Get(url)
		if err != nil {
			return LinkResult{URL: url, Err: err}
		}
	}
	defer resp.Body.Close()
	return LinkResult{URL: url, StatusCode: resp.StatusCode}
}

// checkLinks concurrently validates all provided URLs
func checkLinks(links []string) []LinkResult {
	client := &http.Client{
		Timeout: httpTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	sem := make(chan struct{}, maxConcurrent)
	results := make([]LinkResult, len(links))
	var wg sync.WaitGroup

	for i, link := range links {
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[idx] = checkLink(client, url)
		}(i, link)
	}

	wg.Wait()
	return results
}

func main() {
	fmt.Printf("Extracting links from %s...\n", readmeFile)

	links, err := extractLinks(readmeFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Found %d unique links. Checking...\n", len(links))

	results := checkLinks(links)

	failed := 0
	for _, result := range results {
		if result.Err != nil {
			fmt.Printf("[ERROR] %s — %v\n", result.URL, result.Err)
			failed++
		} else if result.StatusCode >= 400 {
			fmt.Printf("[%d]   %s\n", result.StatusCode, result.URL)
			failed++
		}
	}

	fmt.Printf("\nDone. %d/%d links failed.\n", failed, len(results))
	if failed > 0 {
		os.Exit(1)
	}
}
