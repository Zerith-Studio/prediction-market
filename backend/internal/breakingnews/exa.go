// Package breakingnews generates the markets-index "Breaking News" panel: once
// an hour, for each relevant match, it pulls a REAL, recent web article from the
// Exa search API and pairs it with the match's market Yes% + momentum delta from
// real odds. Nothing is fabricated — a match with no relevant fresh article gets
// no news row. The Searcher/Summarizer/OddsSource seams keep the whole feature
// optional (no EXA_API_KEY, no ticker) and tests hermetic.
package breakingnews

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Article is one real search result. Every field traces to a real web page.
type Article struct {
	Title       string
	URL         string
	Source      string // domain, e.g. "goal.com"
	Highlight   string // real excerpt/highlight
	PublishedAt *time.Time
}

// Searcher finds recent web articles for a query. Behind an interface so tests
// use a fake server. Satisfied by *Exa.
type Searcher interface {
	Search(ctx context.Context, query string, sinceHours int) ([]Article, error)
}

// Exa calls the Exa search API (https://exa.ai). No SDK dep.
type Exa struct {
	APIKey string
	Base   string
	Client *http.Client
}

func NewExa(apiKey string) *Exa {
	return &Exa{APIKey: apiKey, Base: "https://api.exa.ai", Client: &http.Client{Timeout: 30 * time.Second}}
}

func (e *Exa) Search(ctx context.Context, query string, sinceHours int) ([]Article, error) {
	req := map[string]any{
		"query":      query,
		"numResults": 6,
		"type":       "auto",
		"contents": map[string]any{
			"text":       map[string]any{"maxCharacters": 500},
			"highlights": map[string]any{"numSentences": 2, "highlightsPerUrl": 1},
		},
	}
	if sinceHours > 0 {
		req["startPublishedDate"] = time.Now().Add(-time.Duration(sinceHours) * time.Hour).UTC().Format(time.RFC3339)
	}
	raw, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Base+"/search", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("x-api-key", e.APIKey)
	httpReq.Header.Set("content-type", "application/json")

	resp, err := e.Client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("exa: status %d", resp.StatusCode)
	}
	var out struct {
		Results []struct {
			Title         string   `json:"title"`
			URL           string   `json:"url"`
			PublishedDate string   `json:"publishedDate"`
			Highlights    []string `json:"highlights"`
			Text          string   `json:"text"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	arts := make([]Article, 0, len(out.Results))
	for _, r := range out.Results {
		a := Article{Title: strings.TrimSpace(r.Title), URL: r.URL, Source: domainOf(r.URL)}
		if len(r.Highlights) > 0 {
			a.Highlight = strings.TrimSpace(r.Highlights[0])
		} else {
			a.Highlight = truncate(r.Text, 240)
		}
		if r.PublishedDate != "" {
			// Exa stamps fractional seconds ("…16.000Z"); RFC3339Nano parses both
			// that and the plain "…16Z" form.
			if t, err := time.Parse(time.RFC3339Nano, r.PublishedDate); err == nil {
				a.PublishedAt = &t
			}
		}
		if a.Title != "" && a.URL != "" {
			arts = append(arts, a)
		}
	}
	return arts, nil
}

func domainOf(u string) string {
	p, err := url.Parse(u)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(p.Hostname(), "www.")
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n]) + "…"
}
