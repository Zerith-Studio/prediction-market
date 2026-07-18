package breakingnews

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Summarizer condenses a real article into ONE grounded sentence. Optional — nil
// means the panel shows the raw Exa highlight verbatim. It must never add facts
// beyond the headline/excerpt it's given. Satisfied by *GeminiSummarizer.
type Summarizer interface {
	Summarize(ctx context.Context, home, away, headline, highlight string) (string, error)
}

// GeminiSummarizer calls Google's generateContent API (same shape the oneliner
// package uses). No SDK dep.
type GeminiSummarizer struct {
	APIKey string
	Model  string
	Client *http.Client
}

func NewGeminiSummarizer(apiKey, model string) *GeminiSummarizer {
	if model == "" {
		model = "gemini-3.5-flash"
	}
	return &GeminiSummarizer{APIKey: apiKey, Model: model, Client: &http.Client{Timeout: 30 * time.Second}}
}

func (g *GeminiSummarizer) Summarize(ctx context.Context, home, away, headline, highlight string) (string, error) {
	prompt := fmt.Sprintf(
		"Football match %s vs %s. A news article says:\nHeadline: %q\nExcerpt: %q\n\n"+
			"Write ONE concise present-tense sentence (max 22 words) giving the single most "+
			"relevant fact for someone following this match. Use ONLY facts stated in the "+
			"headline/excerpt above — do not add, infer, or invent anything. No hashtags, no "+
			"emoji, no quotes. Return just the sentence.",
		home, away, headline, highlight)

	body, _ := json.Marshal(map[string]any{
		"contents": []map[string]any{{"parts": []map[string]string{{"text": prompt}}}},
	})
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		g.Model, g.APIKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")

	resp, err := g.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("breakingnews: gemini status %d", resp.StatusCode)
	}
	var out struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Candidates) == 0 || len(out.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("breakingnews: empty gemini response")
	}
	return strings.TrimSpace(out.Candidates[0].Content.Parts[0].Text), nil
}
