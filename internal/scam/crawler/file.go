package crawler

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LoadURLs reads a seed file of one url per line. Blank lines and lines whose
// first non-space character is '#' (comments) are skipped. Surrounding
// whitespace is trimmed.
func LoadURLs(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("crawler: open seeds %q: %w", path, err)
	}
	defer f.Close()

	var urls []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		urls = append(urls, line)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("crawler: read seeds %q: %w", path, err)
	}
	return urls, nil
}
