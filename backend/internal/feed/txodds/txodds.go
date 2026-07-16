// Package txodds implements feed.FeedProvider against the real TxLINE API
// (txline.txodds.com — devnet server for the hackathon). Access is
// self-provisioned via the free World Cup tier (see subscribe.go): guest JWT +
// on-chain subscribe + activation, cached on disk.
//
// Data mapping (only markets TxLINE actually prices are quoted by the bot):
//
//	ASIANHANDICAP_PARTICIPANT_GOALS line=0  → template "dnb_home" (draw no bet)
//	OVERUNDER_PARTICIPANT_GOALS line=2.5    → template "over_2_5"
//	OVERUNDER_PARTICIPANT_GOALS line=0.75 half=1 → template "ou_1h_075"
//
// Prices come from the demargined "StablePrice" feed: Pct when present, else
// 1/decimal-odds normalized. Scores stream actions map to kickoff / score /
// full_time events with running goals tracked per fixture.
package txodds

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"

	"github.com/Zerith-Studio/prediction-market/backend/internal/feed"
)

var ErrNotConfigured = errors.New("txodds: provider not configured (wallet keypair required)")

// Fixture is one TxLINE fixture (real teams, real kickoff).
type Fixture struct {
	ID          int64
	Home, Away  string
	Kickoff     time.Time
	Competition string
	Live        bool
}

type Provider struct {
	Base    string
	RPCURL  string
	Cache   string
	Wallet  solana.PrivateKey
	Log     *slog.Logger
	Client  *http.Client
	creds   *Credentials
	credsMu sync.Mutex

	mu    sync.Mutex
	subs  map[string][]chan feed.MatchEvent // fixtureID → subscribers
	score map[string]*scoreState            // running score per fixture
	pumps bool
}

type scoreState struct {
	homeID, awayID int64
	home, away     int
	htHome, htAway int
	minute         int
	kickedOff      bool
	finished       bool
}

// New provisions credentials (or loads the cache) and returns a live provider.
func New(base, rpcURL, cachePath string, wallet solana.PrivateKey, log *slog.Logger) (*Provider, error) {
	if len(wallet) == 0 {
		return nil, ErrNotConfigured
	}
	if base == "" {
		base = DevNetBase
	}
	p := &Provider{
		Base:   base,
		RPCURL: rpcURL,
		Cache:  cachePath,
		Wallet: wallet,
		Log:    log,
		Client: &http.Client{Timeout: 30 * time.Second},
		subs:   make(map[string][]chan feed.MatchEvent),
		score:  make(map[string]*scoreState),
	}
	creds, err := EnsureCredentials(context.Background(), base, rpcURL, cachePath, wallet)
	if err != nil {
		return nil, err
	}
	p.creds = creds
	return p, nil
}

// --- REST ---------------------------------------------------------------------

func (p *Provider) get(ctx context.Context, path string, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.Base+path, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+p.creds.JWT)
		req.Header.Set("X-Api-Token", p.creds.APIToken)
		res, err := p.Client.Do(req)
		if err != nil {
			return err
		}
		if res.StatusCode == http.StatusUnauthorized && attempt == 0 {
			res.Body.Close()
			p.credsMu.Lock()
			err := p.creds.RenewJWT(ctx, p.Base)
			p.credsMu.Unlock()
			if err != nil {
				return err
			}
			continue
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(res.Body)
			return fmt.Errorf("txodds: GET %s → %d: %s", path, res.StatusCode, truncate(body))
		}
		return json.NewDecoder(res.Body).Decode(out)
	}
	return fmt.Errorf("txodds: unauthorized after JWT renewal")
}

type wireFixture struct {
	StartTime         int64  `json:"StartTime"`
	Competition       string `json:"Competition"`
	CompetitionID     int    `json:"CompetitionId"`
	Participant1      string `json:"Participant1"`
	Participant2      string `json:"Participant2"`
	Participant1Home  bool   `json:"Participant1IsHome"`
	FixtureID         int64  `json:"FixtureId"`
	GameState         any    `json:"GameState"`
}

// Fixtures lists upcoming fixtures, optionally filtered by competition
// (72 = World Cup).
func (p *Provider) Fixtures(ctx context.Context, competitionID int) ([]Fixture, error) {
	var wire []wireFixture
	path := "/api/fixtures/snapshot"
	if competitionID > 0 {
		path += "?competitionId=" + strconv.Itoa(competitionID)
	}
	if err := p.get(ctx, path, &wire); err != nil {
		return nil, err
	}
	out := make([]Fixture, 0, len(wire))
	for _, w := range wire {
		home, away := w.Participant1, w.Participant2
		if !w.Participant1Home {
			home, away = away, home
		}
		out = append(out, Fixture{
			ID:          w.FixtureID,
			Home:        home,
			Away:        away,
			Kickoff:     time.UnixMilli(w.StartTime).UTC(),
			Competition: w.Competition,
		})
	}
	return out, nil
}

// --- odds mapping --------------------------------------------------------------

type wireOdds struct {
	FixtureID        int64    `json:"FixtureId"`
	SuperOddsType    string   `json:"SuperOddsType"`
	MarketParameters string   `json:"MarketParameters"`
	MarketPeriod     *string  `json:"MarketPeriod"`
	PriceNames       []string `json:"PriceNames"`
	Prices           []int64  `json:"Prices"` // decimal odds × 1000
	Pct              []string `json:"Pct"`    // demargined implied %, or "NA"
}

// templateFor maps a TxLINE market to our template key ("" = unmapped).
func templateFor(o wireOdds) string {
	period := ""
	if o.MarketPeriod != nil {
		period = *o.MarketPeriod
	}
	switch {
	case o.SuperOddsType == "ASIANHANDICAP_PARTICIPANT_GOALS" && o.MarketParameters == "line=0" && period == "":
		return "dnb_home"
	case o.SuperOddsType == "OVERUNDER_PARTICIPANT_GOALS" && o.MarketParameters == "line=2.5" && period == "":
		return "over_2_5"
	case o.SuperOddsType == "OVERUNDER_PARTICIPANT_GOALS" && o.MarketParameters == "line=0.75" && period == "half=1":
		return "ou_1h_075"
	}
	return ""
}

// impliedCents returns the YES price in cents for the market's first outcome
// (part1/over), preferring the feed's demargined Pct.
func impliedCents(o wireOdds) (uint16, bool) {
	if len(o.Pct) > 0 && o.Pct[0] != "NA" {
		if f, err := strconv.ParseFloat(o.Pct[0], 64); err == nil {
			return clampCents(f), true
		}
	}
	if len(o.Prices) == 2 && o.Prices[0] > 0 && o.Prices[1] > 0 {
		a := 1000.0 / float64(o.Prices[0])
		b := 1000.0 / float64(o.Prices[1])
		return clampCents(a / (a + b) * 100), true
	}
	return 0, false
}

func clampCents(f float64) uint16 {
	c := math.Round(f)
	if c < 1 {
		c = 1
	}
	if c > 99 {
		c = 99
	}
	return uint16(c)
}

// OddsSnapshot fetches the fixture's current odds snapshot and maps it to
// implied YES prices in cents per template key — the same mapping the live
// stream applies (templateFor + impliedCents), but returned synchronously for
// on-demand callers (the admin panel). Only the markets TxLINE actually prices
// appear in the result.
func (p *Provider) OddsSnapshot(ctx context.Context, fixtureID string) (map[string]uint16, error) {
	var odds []wireOdds
	if err := p.get(ctx, "/api/odds/snapshot/"+fixtureID, &odds); err != nil {
		return nil, err
	}
	out := make(map[string]uint16, len(odds))
	for _, o := range odds {
		key := templateFor(o)
		if key == "" {
			continue
		}
		if price, ok := impliedCents(o); ok {
			out[key] = price
		}
	}
	return out, nil
}

// CredentialsExpiry reports when the cached TxLINE credentials expire (for the
// admin ops dashboard). Zero time when credentials are unset.
func (p *Provider) CredentialsExpiry() time.Time {
	p.credsMu.Lock()
	defer p.credsMu.Unlock()
	if p.creds == nil {
		return time.Time{}
	}
	return p.creds.ExpiresAt
}

// --- streaming -----------------------------------------------------------------

// Stream subscribes to one fixture's normalized event stream. Shared SSE pumps
// (one for odds, one for scores, covering ALL fixtures) start lazily with the
// first subscriber; events are demuxed by FixtureId.
func (p *Provider) Stream(ctx context.Context, fixtureID string) (<-chan feed.MatchEvent, error) {
	ch := make(chan feed.MatchEvent, 64)
	p.mu.Lock()
	p.subs[fixtureID] = append(p.subs[fixtureID], ch)
	if p.score[fixtureID] == nil {
		p.score[fixtureID] = &scoreState{}
	}
	start := !p.pumps
	p.pumps = true
	p.mu.Unlock()

	if start {
		go p.pump(context.WithoutCancel(ctx), "/api/odds/stream", p.handleOddsFrame)
		go p.pump(context.WithoutCancel(ctx), "/api/scores/stream", p.handleScoreFrame)
	}

	// Seed with the current snapshots so subscribers start warm.
	go p.emitSnapshots(ctx, fixtureID)

	go func() {
		<-ctx.Done()
		p.mu.Lock()
		subs := p.subs[fixtureID]
		for i, c := range subs {
			if c == ch {
				p.subs[fixtureID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		p.mu.Unlock()
		close(ch)
	}()
	return ch, nil
}

func (p *Provider) emitSnapshots(ctx context.Context, fixtureID string) {
	var odds []wireOdds
	if err := p.get(ctx, "/api/odds/snapshot/"+fixtureID, &odds); err == nil {
		for _, o := range odds {
			p.handleOdds(o)
		}
	} else {
		p.Log.Warn("txodds: odds snapshot", "fixture", fixtureID, "err", err)
	}
	var scores []wireScore
	if err := p.get(ctx, "/api/scores/snapshot/"+fixtureID, &scores); err == nil {
		for _, s := range scores {
			p.handleScore(s)
		}
	}
}

// pump maintains one SSE connection with reconnect + JWT renewal.
func (p *Provider) pump(ctx context.Context, path string, handle func([]byte)) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := p.streamOnce(ctx, path, handle)
		if ctx.Err() != nil {
			return
		}
		p.Log.Warn("txodds: stream dropped — reconnecting", "path", path, "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (p *Provider) streamOnce(ctx context.Context, path string, handle func([]byte)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.Base+path, nil)
	if err != nil {
		return err
	}
	p.credsMu.Lock()
	req.Header.Set("Authorization", "Bearer "+p.creds.JWT)
	req.Header.Set("X-Api-Token", p.creds.APIToken)
	p.credsMu.Unlock()
	req.Header.Set("Accept", "text/event-stream")

	client := &http.Client{} // no timeout: SSE is long-lived
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		p.credsMu.Lock()
		rerr := p.creds.RenewJWT(ctx, p.Base)
		p.credsMu.Unlock()
		if rerr != nil {
			return rerr
		}
		return fmt.Errorf("auth rejected (%d), JWT renewed", res.StatusCode)
	}
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 300))
		return fmt.Errorf("status %d: %s", res.StatusCode, body)
	}

	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if payload, ok := strings.CutPrefix(line, "data:"); ok {
			data := strings.TrimSpace(payload)
			if data != "" {
				handle([]byte(data))
			}
		}
	}
	return sc.Err()
}

func (p *Provider) handleOddsFrame(b []byte) {
	var o wireOdds
	if json.Unmarshal(b, &o) != nil || o.FixtureID == 0 {
		return
	}
	p.handleOdds(o)
}

func (p *Provider) handleOdds(o wireOdds) {
	key := templateFor(o)
	if key == "" {
		return
	}
	price, ok := impliedCents(o)
	if !ok {
		return
	}
	fixtureID := strconv.FormatInt(o.FixtureID, 10)
	p.broadcast(fixtureID, feed.MatchEvent{
		FixtureID: fixtureID,
		Type:      feed.EventOdds,
		Payload:   map[string]any{"prices": map[string]uint16{key: price}},
	})
}

type wireScore struct {
	FixtureID      int64          `json:"FixtureId"`
	GameState      any            `json:"GameState"`
	Action         string         `json:"Action"`
	Participant1ID int64          `json:"Participant1Id"`
	Participant2ID int64          `json:"Participant2Id"`
	Participant1H  bool           `json:"Participant1IsHome"`
	Data           map[string]any `json:"Data"`
	Stats          map[string]any `json:"Stats"`
}

func (p *Provider) handleScoreFrame(b []byte) {
	var s wireScore
	if json.Unmarshal(b, &s) != nil || s.FixtureID == 0 {
		return
	}
	p.handleScore(s)
}

// handleScore folds score actions into per-fixture running state and emits
// kickoff / score / full_time events. Stats (encoded period_prefix+base_key:
// 0=total, 1000=H1; key 1/2 = home/away goals) override the action-counted
// score when present.
func (p *Provider) handleScore(s wireScore) {
	fixtureID := strconv.FormatInt(s.FixtureID, 10)
	p.mu.Lock()
	st := p.score[fixtureID]
	if st == nil {
		p.mu.Unlock()
		return // not a fixture we cover
	}
	if st.homeID == 0 {
		if s.Participant1H {
			st.homeID, st.awayID = s.Participant1ID, s.Participant2ID
		} else {
			st.homeID, st.awayID = s.Participant2ID, s.Participant1ID
		}
	}

	action := strings.ToLower(s.Action)
	var emit feed.EventType

	switch {
	case action == "kickoff" || (action == "period_start" && !st.kickedOff):
		if !st.kickedOff {
			st.kickedOff = true
			emit = feed.EventKickoff
		}
	case action == "goal":
		if pid := participantOf(s.Data); pid != 0 {
			if pid == st.awayID {
				st.away++
			} else {
				st.home++
			}
		}
		emit = feed.EventScore
	case action == "halftime_finalised" || action == "period_end":
		if st.htHome == 0 && st.htAway == 0 {
			st.htHome, st.htAway = st.home, st.away
		}
		emit = feed.EventScore
	case action == "final_whistle" || gameStateFinished(s.GameState):
		if !st.finished {
			st.finished = true
			emit = feed.EventFullTime
		}
	}
	// Stats carry authoritative totals (period_prefix+base_key) — applied
	// after the action so they override the local goal counter.
	applyStats(st, s.Stats)
	var payload map[string]any
	if emit != "" {
		if emit == feed.EventKickoff {
			payload = map[string]any{"minute": 0}
		} else {
			payload = scorePayload(st)
		}
	}
	p.mu.Unlock()

	if emit != "" {
		p.broadcast(fixtureID, feed.MatchEvent{FixtureID: fixtureID, Type: emit, Payload: payload})
	}
}

func scorePayload(st *scoreState) map[string]any {
	return map[string]any{
		"home_goals":    st.home,
		"away_goals":    st.away,
		"ht_home_goals": st.htHome,
		"ht_away_goals": st.htAway,
		"minute":        st.minute,
	}
}

func applyStats(st *scoreState, stats map[string]any) {
	get := func(key string) (int, bool) {
		v, ok := stats[key]
		if !ok {
			return 0, false
		}
		switch n := v.(type) {
		case float64:
			return int(n), true
		case string:
			i, err := strconv.Atoi(n)
			return i, err == nil
		}
		return 0, false
	}
	if v, ok := get("1"); ok {
		st.home = v
	}
	if v, ok := get("2"); ok {
		st.away = v
	}
	if v, ok := get("1001"); ok {
		st.htHome = v
	}
	if v, ok := get("1002"); ok {
		st.htAway = v
	}
}

func participantOf(data map[string]any) int64 {
	for _, k := range []string{"ParticipantId", "Participant"} {
		if v, ok := data[k]; ok {
			if f, ok := v.(float64); ok {
				return int64(f)
			}
		}
	}
	return 0
}

func gameStateFinished(gs any) bool {
	switch v := gs.(type) {
	case string:
		return strings.EqualFold(v, "finished")
	case float64:
		return int(v) == 5 // phase encoding: 5 = Finished
	}
	return false
}

func (p *Provider) broadcast(fixtureID string, ev feed.MatchEvent) {
	p.mu.Lock()
	subs := append([]chan feed.MatchEvent(nil), p.subs[fixtureID]...)
	p.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- ev:
		default: // slow consumer: drop rather than stall the pump
		}
	}
}

// --- reconciliation snapshot -------------------------------------------------

// Result is a fixture's folded final state from the score snapshot.
type Result struct {
	Home, Away     int
	HTHome, HTAway int
	TotalPasses    *int // nil — txodds passes stat isn't mapped yet (admin can supply)
}

// FinalState folds the fixture's score snapshot into a final result and reports
// whether the feed considers the match finished (GameState finished / final
// whistle). Unlike the live SSE stream, the snapshot is authoritative and
// queryable at any time — this is what lets the resolution reconciler recover a
// full-time event that was missed while the service was down or the stream
// dropped.
func (p *Provider) FinalState(ctx context.Context, fixtureID string) (Result, bool, error) {
	var frames []wireScore
	if err := p.get(ctx, "/api/scores/snapshot/"+fixtureID, &frames); err != nil {
		return Result{}, false, err
	}
	st := &scoreState{}
	finished := false
	for _, s := range frames {
		if st.homeID == 0 && (s.Participant1ID != 0 || s.Participant2ID != 0) {
			if s.Participant1H {
				st.homeID, st.awayID = s.Participant1ID, s.Participant2ID
			} else {
				st.homeID, st.awayID = s.Participant2ID, s.Participant1ID
			}
		}
		switch strings.ToLower(s.Action) {
		case "goal":
			if pid := participantOf(s.Data); pid != 0 {
				if pid == st.awayID {
					st.away++
				} else {
					st.home++
				}
			}
		case "halftime_finalised", "period_end":
			if st.htHome == 0 && st.htAway == 0 {
				st.htHome, st.htAway = st.home, st.away
			}
		case "final_whistle":
			finished = true
		}
		applyStats(st, s.Stats)
		if gameStateFinished(s.GameState) {
			finished = true
		}
	}
	return Result{Home: st.home, Away: st.away, HTHome: st.htHome, HTAway: st.htAway}, finished, nil
}

func truncate(b []byte) string {
	if len(b) > 200 {
		return string(b[:200]) + "…"
	}
	return string(b)
}
