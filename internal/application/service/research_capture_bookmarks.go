package service

import (
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
	"golang.org/x/net/html"
)

const ResearchCaptureMaxItems = 5000
const ResearchCaptureMaxHTML = 5 << 20

func canonicalBookmarkURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u == nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("only http(s) bookmark URLs without credentials are supported")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""
	return u.String(), nil
}

// ParseResearchBookmarks consumes Netscape exports without fetching any URLs.
func ParseResearchBookmarks(source string) ([]types.ResearchCaptureItem, int, error) {
	if len(source) > ResearchCaptureMaxHTML {
		return nil, 0, fmt.Errorf("bookmark file exceeds 5 MiB")
	}
	z := html.NewTokenizer(strings.NewReader(source))
	items := []types.ResearchCaptureItem{}
	seen := map[string]bool{}
	folders := []string{}
	pendingFolder := ""
	capture := ""
	text := strings.Builder{}
	var current types.ResearchCaptureItem
	skipped := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() != io.EOF {
				return nil, 0, z.Err()
			}
			break
		}
		switch tt {
		case html.StartTagToken:
			token := z.Token()
			switch token.Data {
			case "h3":
				capture = "h3"
				text.Reset()
			case "dl":
				folders = append(folders, pendingFolder)
				pendingFolder = ""
			case "a":
				capture = "a"
				text.Reset()
				current = types.ResearchCaptureItem{Status: "queued"}
				for _, a := range token.Attr {
					switch a.Key {
					case "href":
						current.URL = a.Val
					case "add_date":
						current.AddedAt, _ = strconv.ParseInt(a.Val, 10, 64)
					}
				}
			}
		case html.TextToken:
			if capture != "" {
				text.Write(z.Text())
			}
		case html.EndTagToken:
			name := z.Token().Data
			switch name {
			case "h3":
				if capture == "h3" {
					pendingFolder = strings.TrimSpace(text.String())
					capture = ""
				}
			case "dl":
				if len(folders) > 0 {
					folders = folders[:len(folders)-1]
				}
			case "a":
				if capture != "a" {
					continue
				}
				capture = ""
				current.Title = strings.TrimSpace(text.String())
				clean, err := canonicalBookmarkURL(current.URL)
				if err != nil || seen[clean] {
					skipped++
					continue
				}
				seen[clean] = true
				current.URL = clean
				parts := []string{}
				for _, folder := range folders {
					if folder != "" {
						parts = append(parts, folder)
					}
				}
				current.Folder = strings.Join(parts, " / ")
				items = append(items, current)
				if len(items) > ResearchCaptureMaxItems {
					return nil, skipped, fmt.Errorf("at most %d bookmarks per import", ResearchCaptureMaxItems)
				}
			}
		}
	}
	if len(items) == 0 {
		return nil, skipped, fmt.Errorf("no supported bookmarks found")
	}
	return items, skipped, nil
}
