package store

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"hash"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/klauspost/compress/zstd"
)

const tokenizerVersion = "stalker-lex-v1"

type tokenStats struct {
	tokenCount int
	byteCount  int64
	charCount  int64
	digest     string
	values     map[string]tokenAggregate
}

type tokenAggregate struct {
	value         string
	occurrences   int
	firstPosition int
}

type tokenCollector struct {
	encoding  string
	raw       bytes.Buffer
	stats     tokenStats
	digest    hash.Hash
	token     strings.Builder
	pending   []byte
	finalized bool
}

func newTokenCollector(encoding string) *tokenCollector {
	return &tokenCollector{
		encoding: normalizedEncoding(encoding),
		stats:    tokenStats{values: map[string]tokenAggregate{}},
		digest:   sha256.New(),
	}
}

func (c *tokenCollector) SetEncoding(encoding string) {
	c.encoding = normalizedEncoding(encoding)
}

func (c *tokenCollector) Write(chunk []byte) {
	if len(chunk) == 0 || c.finalized {
		return
	}
	if c.encoded() {
		_, _ = c.raw.Write(chunk)
		return
	}
	c.writePlain(chunk)
}

func (c *tokenCollector) Stats(final bool) (tokenStats, error) {
	if final {
		if err := c.finalize(); err != nil {
			return tokenStats{}, err
		}
	}
	return cloneTokenStats(c.stats), nil
}

func (c *tokenCollector) finalize() error {
	if c.finalized {
		return nil
	}
	if c.encoded() {
		stats, err := tokenizeEncodedBytes(c.raw.Bytes(), c.encoding)
		c.raw.Reset()
		if err != nil {
			return err
		}
		c.stats = stats
		c.finalized = true
		return nil
	}
	if len(c.pending) > 0 {
		for len(c.pending) > 0 {
			rn, size := utf8.DecodeRune(c.pending)
			if rn == utf8.RuneError && size == 1 {
				rn = unicode.ReplacementChar
			}
			c.addRune(rn, size)
			c.pending = c.pending[size:]
		}
	}
	c.flushToken()
	c.stats.digest = hex.EncodeToString(c.digest.Sum(nil))
	c.finalized = true
	return nil
}

func (c *tokenCollector) encoded() bool {
	return c.encoding != "" && c.encoding != "identity"
}

func (c *tokenCollector) writePlain(chunk []byte) {
	data := chunk
	if len(c.pending) > 0 {
		merged := make([]byte, 0, len(c.pending)+len(chunk))
		merged = append(merged, c.pending...)
		merged = append(merged, chunk...)
		data = merged
		c.pending = nil
	}
	for len(data) > 0 {
		if !utf8.FullRune(data) {
			c.pending = append(c.pending[:0], data...)
			return
		}
		rn, size := utf8.DecodeRune(data)
		c.addRune(rn, size)
		data = data[size:]
	}
}

func (c *tokenCollector) addRune(rn rune, size int) {
	c.stats.charCount++
	c.stats.byteCount += int64(size)
	switch {
	case isTokenRune(rn):
		c.token.WriteRune(rn)
	case unicode.IsSpace(rn):
		c.flushToken()
	default:
		c.flushToken()
		c.stats.addToken(string(rn), c.digest)
	}
}

func (c *tokenCollector) flushToken() {
	if c.token.Len() == 0 {
		return
	}
	c.stats.addToken(c.token.String(), c.digest)
	c.token.Reset()
}

func tokenizeEncodedBytes(raw []byte, encoding string) (tokenStats, error) {
	r, err := decodedReader(bytes.NewReader(raw), encoding)
	if err != nil {
		return tokenStats{}, err
	}
	defer r.Close()
	return tokenizeReader(r)
}

func decodedReader(r io.Reader, encoding string) (io.ReadCloser, error) {
	switch normalizedEncoding(encoding) {
	case "", "identity":
		return io.NopCloser(r), nil
	case "gzip":
		return gzip.NewReader(r)
	case "zstd":
		decoder, err := zstd.NewReader(r)
		if err != nil {
			return nil, err
		}
		return &compoundReadCloser{
			Reader:  decoder,
			closers: []io.Closer{closerFunc(func() error { decoder.Close(); return nil })},
		}, nil
	default:
		return io.NopCloser(r), nil
	}
}

func normalizedEncoding(encoding string) string {
	return strings.ToLower(strings.TrimSpace(encoding))
}

func cloneTokenStats(stats tokenStats) tokenStats {
	out := stats
	out.values = make(map[string]tokenAggregate, len(stats.values))
	for key, value := range stats.values {
		out.values[key] = value
	}
	return out
}

func tokenizeReader(r io.Reader) (tokenStats, error) {
	stats := tokenStats{values: map[string]tokenAggregate{}}
	digest := sha256.New()
	reader := bufio.NewReader(r)
	var token strings.Builder

	flush := func() {
		if token.Len() == 0 {
			return
		}
		stats.addToken(token.String(), digest)
		token.Reset()
	}

	for {
		rn, size, err := reader.ReadRune()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return tokenStats{}, err
		}
		stats.charCount++
		stats.byteCount += int64(size)

		switch {
		case isTokenRune(rn):
			token.WriteRune(rn)
		case unicode.IsSpace(rn):
			flush()
		default:
			flush()
			stats.addToken(string(rn), digest)
		}
	}
	flush()
	stats.digest = hex.EncodeToString(digest.Sum(nil))
	return stats, nil
}

func (s *tokenStats) addToken(value string, digest io.Writer) {
	tokenHash := hashToken(value)
	if agg, ok := s.values[tokenHash]; ok {
		agg.occurrences++
		s.values[tokenHash] = agg
	} else {
		s.values[tokenHash] = tokenAggregate{
			value:         value,
			occurrences:   1,
			firstPosition: s.tokenCount,
		}
	}
	_, _ = digest.Write([]byte(value))
	_, _ = digest.Write([]byte{0})
	s.tokenCount++
}

func isTokenRune(rn rune) bool {
	return unicode.IsLetter(rn) || unicode.IsDigit(rn) || rn == '_' || rn == '-' || rn == '\''
}

func hashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Store) upsertTokenStats(ctx context.Context, exchangeID, side string, stats tokenStats, aggregate bool) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
INSERT INTO llm_token_runs (
  exchange_id, side, tokenizer, token_count, unique_token_count,
  byte_count, char_count, digest_sha256, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(exchange_id, side, tokenizer) DO UPDATE SET
  token_count = excluded.token_count,
  unique_token_count = excluded.unique_token_count,
  byte_count = excluded.byte_count,
  char_count = excluded.char_count,
  digest_sha256 = excluded.digest_sha256,
  updated_at = excluded.updated_at`,
		exchangeID,
		side,
		tokenizerVersion,
		stats.tokenCount,
		len(stats.values),
		stats.byteCount,
		stats.charCount,
		stats.digest,
		now,
	); err != nil {
		return err
	}

	if aggregate {
		stmt, err := tx.PrepareContext(ctx, `
INSERT INTO llm_token_totals (
  side, tokenizer, token_value, token_hash, occurrences, first_seen_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(side, tokenizer, token_hash) DO UPDATE SET
  token_value = CASE
    WHEN llm_token_totals.token_value = '' THEN excluded.token_value
    ELSE llm_token_totals.token_value
  END,
  occurrences = llm_token_totals.occurrences + excluded.occurrences,
  updated_at = excluded.updated_at`)
		if err != nil {
			return err
		}
		defer stmt.Close()

		for tokenHash, agg := range stats.values {
			if _, err := stmt.ExecContext(ctx, side, tokenizerVersion, agg.value, tokenHash, agg.occurrences, now, now); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) TokenReport(ctx context.Context, exchangeID string, limit int) (TokenReport, error) {
	runs, err := s.tokenRuns(ctx, exchangeID)
	if err != nil {
		return TokenReport{}, err
	}
	values, err := s.tokenValues(ctx, exchangeID, limit)
	if err != nil {
		return TokenReport{}, err
	}
	return TokenReport{Runs: runs, Values: values}, nil
}

func (s *Store) TokenTotals(ctx context.Context) (TokenTotals, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT side, COALESCE(SUM(token_count), 0)
FROM llm_token_runs
GROUP BY side`)
	if err != nil {
		return TokenTotals{}, err
	}
	defer rows.Close()

	var totals TokenTotals
	for rows.Next() {
		var side string
		var count int64
		if err := rows.Scan(&side, &count); err != nil {
			return TokenTotals{}, err
		}
		switch side {
		case "input":
			totals.InputTokens = count
		case "output":
			totals.OutputTokens = count
		}
	}
	if err := rows.Err(); err != nil {
		return TokenTotals{}, err
	}
	top, err := s.TopTokens(ctx, 12)
	if err != nil {
		return TokenTotals{}, err
	}
	totals.Top = top
	return totals, nil
}

func (s *Store) TopTokens(ctx context.Context, limit int) (TokenBurns, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	input, err := s.topTokensForSide(ctx, "input", limit)
	if err != nil {
		return TokenBurns{}, err
	}
	output, err := s.topTokensForSide(ctx, "output", limit)
	if err != nil {
		return TokenBurns{}, err
	}
	return TokenBurns{Input: input, Output: output}, nil
}

func (s *Store) topTokensForSide(ctx context.Context, side string, limit int) ([]TokenBurn, error) {
	queryLimit := limit * 20
	if queryLimit < 100 {
		queryLimit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT side, token_value, token_hash, occurrences
FROM llm_token_totals
WHERE side = ? AND tokenizer = ? AND token_value != ''
ORDER BY occurrences DESC, token_value ASC
LIMIT ?`, side, tokenizerVersion, queryLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TokenBurn
	for rows.Next() {
		var burn TokenBurn
		if err := rows.Scan(&burn.Side, &burn.Token, &burn.TokenHash, &burn.Occurrences); err != nil {
			return nil, err
		}
		if !isDisplayTokenValue(burn.Token) {
			continue
		}
		out = append(out, burn)
		if len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

func isDisplayTokenValue(token string) bool {
	for _, rn := range token {
		if unicode.IsLetter(rn) || unicode.IsDigit(rn) {
			return true
		}
	}
	return false
}

func (s *Store) tokenRuns(ctx context.Context, exchangeID string) ([]TokenRun, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT exchange_id, side, tokenizer, token_count, unique_token_count,
  byte_count, char_count, digest_sha256, updated_at
FROM llm_token_runs
WHERE exchange_id = ?
ORDER BY side`, exchangeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TokenRun
	for rows.Next() {
		run, err := scanTokenRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *Store) tokenValues(ctx context.Context, exchangeID string, limit int) ([]TokenValue, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT exchange_id, side, tokenizer, token_value, token_hash, occurrences, first_position
FROM llm_token_values
WHERE exchange_id = ?
ORDER BY occurrences DESC, first_position ASC
	LIMIT ?`, exchangeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TokenValue
	for rows.Next() {
		var value TokenValue
		if err := rows.Scan(
			&value.ExchangeID,
			&value.Side,
			&value.Tokenizer,
			&value.Token,
			&value.TokenHash,
			&value.Occurrences,
			&value.FirstPosition,
		); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func scanTokenRun(row *sql.Rows) (TokenRun, error) {
	var run TokenRun
	var updatedAt string
	err := row.Scan(
		&run.ExchangeID,
		&run.Side,
		&run.Tokenizer,
		&run.TokenCount,
		&run.UniqueTokenCount,
		&run.ByteCount,
		&run.CharCount,
		&run.DigestSHA256,
		&updatedAt,
	)
	if err != nil {
		return TokenRun{}, err
	}
	run.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return run, nil
}
