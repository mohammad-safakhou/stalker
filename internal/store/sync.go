package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type SyncSnapshot struct {
	Device    SyncDevice    `json:"device"`
	Generated time.Time     `json:"generated_at"`
	Cursor    string        `json:"cursor"`
	Totals    TokenTotals   `json:"totals"`
	Live      LiveStats     `json:"live"`
	Hourly    []StatsBucket `json:"hourly"`
	Daily     []StatsBucket `json:"daily"`
}

type SyncHealth struct {
	Device           SyncDevice `json:"device"`
	Generated        time.Time  `json:"generated_at"`
	Status           string     `json:"status"`
	PendingTokenJobs int        `json:"pending_token_jobs"`
	TokenQueueSize   int        `json:"token_queue_size"`
}

type SyncDevice struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Platform string    `json:"platform"`
	LastSeen time.Time `json:"last_seen"`
}

type LiveStats struct {
	WindowSeconds       int     `json:"window_seconds"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
	InputChars          int64   `json:"input_chars"`
	OutputChars         int64   `json:"output_chars"`
	Requests            int64   `json:"requests"`
	Errors              int64   `json:"errors"`
	TokensPerSecond     float64 `json:"tokens_per_second"`
	CharactersPerSecond float64 `json:"characters_per_second"`
	RequestsPerMinute   float64 `json:"requests_per_minute"`
}

type StatsBucket struct {
	Key          string    `json:"key"`
	Granularity  string    `json:"granularity"`
	Start        time.Time `json:"start"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	InputChars   int64     `json:"input_chars"`
	OutputChars  int64     `json:"output_chars"`
	Requests     int64     `json:"requests"`
	Errors       int64     `json:"errors"`
	Streams      int64     `json:"streams"`
}

func (s *Store) SyncSnapshot(ctx context.Context) (SyncSnapshot, error) {
	device, err := s.SyncDevice()
	if err != nil {
		return SyncSnapshot{}, err
	}
	totals, err := s.TokenTotals(ctx)
	if err != nil {
		return SyncSnapshot{}, err
	}
	live, err := s.LiveStats(ctx, time.Now().UTC().Add(-60*time.Second))
	if err != nil {
		return SyncSnapshot{}, err
	}
	hourly, err := s.StatsBuckets(ctx, device.ID, "hourly", 168)
	if err != nil {
		return SyncSnapshot{}, err
	}
	daily, err := s.StatsBuckets(ctx, device.ID, "daily", 90)
	if err != nil {
		return SyncSnapshot{}, err
	}
	cursor, err := s.SyncCursor(ctx)
	if err != nil {
		return SyncSnapshot{}, err
	}
	return SyncSnapshot{
		Device:    device,
		Generated: time.Now().UTC(),
		Cursor:    cursor,
		Totals:    totals,
		Live:      live,
		Hourly:    hourly,
		Daily:     daily,
	}, nil
}

func (s *Store) SyncDevice() (SyncDevice, error) {
	id, err := s.deviceID()
	if err != nil {
		return SyncDevice{}, err
	}
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		name = "Stalker Mac"
	}
	return SyncDevice{
		ID:       id,
		Name:     name,
		Platform: runtime.GOOS,
		LastSeen: time.Now().UTC(),
	}, nil
}

func (s *Store) SyncHealth(ctx context.Context) (SyncHealth, error) {
	device, err := s.SyncDevice()
	if err != nil {
		return SyncHealth{}, err
	}
	if err := s.db.PingContext(ctx); err != nil {
		return SyncHealth{}, err
	}
	queueSize := 0
	pending := 0
	if s.tokenJobs != nil {
		queueSize = cap(s.tokenJobs)
		pending = len(s.tokenJobs)
	}
	return SyncHealth{
		Device:           device,
		Generated:        time.Now().UTC(),
		Status:           "ok",
		PendingTokenJobs: pending,
		TokenQueueSize:   queueSize,
	}, nil
}

func (s *Store) deviceID() (string, error) {
	path := filepath.Join(s.dir, "device_id")
	raw, err := os.ReadFile(path)
	if err == nil {
		if id := strings.TrimSpace(string(raw)); id != "" {
			return id, nil
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}

	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := "stalker-" + hex.EncodeToString(b[:])
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) SyncCursor(ctx context.Context) (string, error) {
	var updated string
	var runs int64
	err := s.db.QueryRowContext(ctx, `
SELECT COALESCE(MAX(updated_at), ''), COUNT(*)
FROM llm_token_runs`).Scan(&updated, &runs)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", updated, runs), nil
}

func (s *Store) LiveStats(ctx context.Context, since time.Time) (LiveStats, error) {
	var stats LiveStats
	stats.WindowSeconds = int(time.Since(since).Seconds())
	if stats.WindowSeconds <= 0 {
		stats.WindowSeconds = 60
	}
	err := s.db.QueryRowContext(ctx, `
SELECT
  COALESCE(SUM(CASE WHEN llm_token_runs.side = 'input' THEN llm_token_runs.token_count ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN llm_token_runs.side = 'output' THEN llm_token_runs.token_count ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN llm_token_runs.side = 'input' THEN llm_token_runs.char_count ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN llm_token_runs.side = 'output' THEN llm_token_runs.char_count ELSE 0 END), 0),
  COUNT(DISTINCT exchanges.id),
  COUNT(DISTINCT CASE WHEN exchanges.error != '' OR exchanges.status_code >= 400 THEN exchanges.id END)
FROM exchanges
LEFT JOIN llm_token_runs ON llm_token_runs.exchange_id = exchanges.id
WHERE exchanges.started_at >= ?`, since.UTC().Format(time.RFC3339Nano)).Scan(
		&stats.InputTokens,
		&stats.OutputTokens,
		&stats.InputChars,
		&stats.OutputChars,
		&stats.Requests,
		&stats.Errors,
	)
	if err != nil {
		return stats, err
	}
	window := float64(stats.WindowSeconds)
	stats.TokensPerSecond = float64(stats.InputTokens+stats.OutputTokens) / window
	stats.CharactersPerSecond = float64(stats.InputChars+stats.OutputChars) / window
	stats.RequestsPerMinute = float64(stats.Requests) / window * 60
	return stats, nil
}

func (s *Store) StatsBuckets(ctx context.Context, deviceID, granularity string, limit int) ([]StatsBucket, error) {
	if limit <= 0 || limit > 1000 {
		limit = 168
	}
	var startExpr string
	switch granularity {
	case "hourly":
		startExpr = `substr(exchanges.started_at, 1, 13) || ':00:00Z'`
	case "daily":
		startExpr = `substr(exchanges.started_at, 1, 10) || 'T00:00:00Z'`
	default:
		return nil, fmt.Errorf("unknown bucket granularity %q", granularity)
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT bucket_start,
  COALESCE(SUM(input_tokens), 0),
  COALESCE(SUM(output_tokens), 0),
  COALESCE(SUM(input_chars), 0),
  COALESCE(SUM(output_chars), 0),
  COUNT(*),
  COALESCE(SUM(error_count), 0),
  COALESCE(SUM(stream_count), 0)
FROM (
  SELECT
    `+startExpr+` AS bucket_start,
    exchanges.id AS exchange_id,
    MAX(CASE WHEN llm_token_runs.side = 'input' THEN llm_token_runs.token_count ELSE 0 END) AS input_tokens,
    MAX(CASE WHEN llm_token_runs.side = 'output' THEN llm_token_runs.token_count ELSE 0 END) AS output_tokens,
    MAX(CASE WHEN llm_token_runs.side = 'input' THEN llm_token_runs.char_count ELSE 0 END) AS input_chars,
    MAX(CASE WHEN llm_token_runs.side = 'output' THEN llm_token_runs.char_count ELSE 0 END) AS output_chars,
    CASE WHEN exchanges.error != '' OR exchanges.status_code >= 400 THEN 1 ELSE 0 END AS error_count,
    CASE WHEN exchanges.is_stream != 0 THEN 1 ELSE 0 END AS stream_count
  FROM exchanges
  LEFT JOIN llm_token_runs ON llm_token_runs.exchange_id = exchanges.id
  GROUP BY bucket_start, exchanges.id
)
GROUP BY bucket_start
ORDER BY bucket_start DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var desc []StatsBucket
	for rows.Next() {
		var bucket StatsBucket
		var start string
		if err := rows.Scan(
			&start,
			&bucket.InputTokens,
			&bucket.OutputTokens,
			&bucket.InputChars,
			&bucket.OutputChars,
			&bucket.Requests,
			&bucket.Errors,
			&bucket.Streams,
		); err != nil {
			return nil, err
		}
		t, err := time.Parse(time.RFC3339, start)
		if err != nil {
			return nil, err
		}
		bucket.Start = t
		bucket.Granularity = granularity
		bucket.Key = deviceID + ":" + granularity + ":" + start
		desc = append(desc, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i, j := 0, len(desc)-1; i < j; i, j = i+1, j-1 {
		desc[i], desc[j] = desc[j], desc[i]
	}
	return desc, nil
}
