package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/klauspost/compress/zstd"
	tiktoken "github.com/pkoukk/tiktoken-go"
)

const (
	countSourceLocalTiktoken = "tiktoken_local"
	defaultEncodingName      = tiktoken.MODEL_O200K_BASE
)

type tokenStats struct {
	tokenizer       string
	countSource     string
	tokenCount      int
	byteCount       int64
	charCount       int64
	wordCount       int
	uniqueWordCount int
	digest          string
	values          map[string]textAggregate
	words           map[string]textAggregate
	chars           map[string]textAggregate
}

type textAggregate struct {
	value         string
	occurrences   int
	firstPosition int
}

type tokenCollector struct {
	encoding  string
	raw       bytes.Buffer
	finalized bool
	side      string
}

type tokenPayload struct {
	side     string
	encoding string
	raw      []byte
}

func newTokenCollector(encoding string) *tokenCollector {
	return &tokenCollector{encoding: normalizedEncoding(encoding)}
}

func (c *tokenCollector) SetEncoding(encoding string) {
	c.encoding = normalizedEncoding(encoding)
}

func (c *tokenCollector) Write(chunk []byte) {
	if len(chunk) == 0 || c.finalized {
		return
	}
	_, _ = c.raw.Write(chunk)
}

func (c *tokenCollector) Payload() tokenPayload {
	c.finalized = true
	return tokenPayload{
		side:     c.side,
		encoding: c.encoding,
		raw:      append([]byte(nil), c.raw.Bytes()...),
	}
}

func (c *tokenCollector) Model() string {
	return c.Payload().Model()
}

func (c *tokenCollector) Stats(final bool, model string) (tokenStats, error) {
	if final {
		c.finalized = true
	}
	return c.Payload().Stats(model)
}

func (p tokenPayload) Model() string {
	raw, err := p.decodedBytes()
	if err != nil {
		return ""
	}
	return extractModel(raw)
}

func (p tokenPayload) Stats(model string) (tokenStats, error) {
	raw, err := p.decodedBytes()
	if err != nil {
		return tokenStats{}, err
	}
	text := extractCountingText(p.side, raw)
	return calculateLocalStats(text, model)
}

func (c *tokenCollector) decodedBytes() ([]byte, error) {
	return c.Payload().decodedBytes()
}

func (p tokenPayload) decodedBytes() ([]byte, error) {
	if len(p.raw) == 0 {
		return nil, nil
	}
	if !p.encoded() {
		return append([]byte(nil), p.raw...), nil
	}
	r, err := decodedReader(bytes.NewReader(p.raw), p.encoding)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}

func (c *tokenCollector) encoded() bool {
	return c.encoding != "" && c.encoding != "identity"
}

func (p tokenPayload) encoded() bool {
	return p.encoding != "" && p.encoding != "identity"
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

func calculateLocalStats(text, model string) (tokenStats, error) {
	encodingName := encodingNameForModel(model)
	enc, err := tiktoken.GetEncoding(encodingName)
	if err != nil {
		return tokenStats{}, err
	}

	stats := tokenStats{
		tokenizer:   "tiktoken:" + encodingName,
		countSource: countSourceLocalTiktoken,
		byteCount:   int64(len([]byte(text))),
		charCount:   int64(utf8.RuneCountInString(text)),
		values:      map[string]textAggregate{},
		words:       map[string]textAggregate{},
		chars:       map[string]textAggregate{},
	}
	digest := sha256.New()

	ids := enc.EncodeOrdinary(text)
	for i, id := range ids {
		value := enc.Decode([]int{id})
		stats.addAggregate(stats.values, value, i)
		_, _ = digest.Write([]byte(value))
		_, _ = digest.Write([]byte{0})
	}
	stats.tokenCount = len(ids)

	for _, word := range extractWords(text) {
		stats.addAggregate(stats.words, word, stats.wordCount)
		stats.wordCount++
	}
	stats.uniqueWordCount = len(stats.words)

	charPos := 0
	for _, rn := range text {
		if unicode.IsSpace(rn) || unicode.IsControl(rn) {
			continue
		}
		stats.addAggregate(stats.chars, string(rn), charPos)
		charPos++
	}

	stats.digest = hex.EncodeToString(digest.Sum(nil))
	return stats, nil
}

func (s *tokenStats) addAggregate(values map[string]textAggregate, value string, position int) {
	hash := hashValue(value)
	if agg, ok := values[hash]; ok {
		agg.occurrences++
		values[hash] = agg
		return
	}
	values[hash] = textAggregate{
		value:         value,
		occurrences:   1,
		firstPosition: position,
	}
}

func extractWords(text string) []string {
	var words []string
	var current strings.Builder
	flush := func() {
		if current.Len() == 0 {
			return
		}
		words = append(words, strings.ToLower(current.String()))
		current.Reset()
	}
	for _, rn := range text {
		switch {
		case unicode.IsLetter(rn) || unicode.IsDigit(rn) || rn == '_' || rn == '-' || rn == '\'':
			current.WriteRune(unicode.ToLower(rn))
		default:
			flush()
		}
	}
	flush()
	return words
}

func extractModel(raw []byte) string {
	var root any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err != nil {
		return ""
	}
	if obj, ok := root.(map[string]any); ok {
		if model, ok := obj["model"].(string); ok {
			return strings.TrimSpace(model)
		}
	}
	return ""
}

func extractCountingText(side string, raw []byte) string {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return ""
	}

	var root any
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	if err := decoder.Decode(&root); err == nil {
		var parts []string
		collectText(root, "", side, &parts)
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}

	if strings.Contains(text, "\ndata:") || strings.HasPrefix(text, "data:") {
		var parts []string
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "" || payload == "[DONE]" {
				continue
			}
			var event any
			decoder := json.NewDecoder(strings.NewReader(payload))
			decoder.UseNumber()
			if err := decoder.Decode(&event); err == nil {
				collectText(event, "", side, &parts)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}

	return text
}

func collectText(value any, key, side string, parts *[]string) {
	switch v := value.(type) {
	case string:
		if shouldCountString(key, side) {
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				*parts = append(*parts, trimmed)
			}
		}
	case []any:
		for _, item := range v {
			collectText(item, key, side, parts)
		}
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lower := strings.ToLower(k)
			if lower == "model" || lower == "id" || lower == "created" || lower == "object" || lower == "status" {
				continue
			}
			collectText(v[k], lower, side, parts)
		}
	}
}

func shouldCountString(key, side string) bool {
	switch strings.ToLower(key) {
	case "", "input", "instructions", "messages", "message", "content", "text", "output", "delta",
		"arguments", "description", "name", "tool", "tools", "function", "prompt", "refusal":
		return true
	case "role", "type", "finish_reason":
		return false
	default:
		return side == "output" && (strings.Contains(key, "text") || strings.Contains(key, "content"))
	}
}

func encodingNameForModel(model string) string {
	model = strings.TrimSpace(model)
	if model != "" {
		if name, ok := tiktoken.MODEL_TO_ENCODING[model]; ok {
			return name
		}
		for prefix, name := range tiktoken.MODEL_PREFIX_TO_ENCODING {
			if strings.HasPrefix(model, prefix) {
				return name
			}
		}
		lower := strings.ToLower(model)
		switch {
		case strings.HasPrefix(lower, "gpt-5"),
			strings.HasPrefix(lower, "gpt-4.5"),
			strings.HasPrefix(lower, "gpt-4.1"),
			strings.HasPrefix(lower, "gpt-4o"),
			strings.HasPrefix(lower, "o1"),
			strings.HasPrefix(lower, "o3"),
			strings.HasPrefix(lower, "o4"),
			strings.Contains(lower, "codex"):
			return tiktoken.MODEL_O200K_BASE
		}
	}
	return defaultEncodingName
}

func hashValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func (s *Store) upsertTokenStats(ctx context.Context, exchangeID, side, provider, model string, stats tokenStats, aggregate bool) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
DELETE FROM llm_token_runs
WHERE exchange_id = ? AND side = ?
  AND (provider != ? OR model != ? OR tokenizer != ? OR count_source != ?)`,
		exchangeID, side, provider, model, stats.tokenizer, stats.countSource,
	); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO llm_token_runs (
  exchange_id, side, provider, model, tokenizer, count_source,
  token_count, unique_token_count, word_count, unique_word_count,
  byte_count, char_count, digest_sha256, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(exchange_id, side, tokenizer) DO UPDATE SET
  provider = excluded.provider,
  model = excluded.model,
  count_source = excluded.count_source,
  token_count = excluded.token_count,
  unique_token_count = excluded.unique_token_count,
  word_count = excluded.word_count,
  unique_word_count = excluded.unique_word_count,
  byte_count = excluded.byte_count,
  char_count = excluded.char_count,
  digest_sha256 = excluded.digest_sha256,
  updated_at = excluded.updated_at`,
		exchangeID,
		side,
		provider,
		model,
		stats.tokenizer,
		stats.countSource,
		stats.tokenCount,
		len(stats.values),
		stats.wordCount,
		stats.uniqueWordCount,
		stats.byteCount,
		stats.charCount,
		stats.digest,
		now,
	); err != nil {
		return err
	}

	if aggregate {
		if err := upsertTextTotals(ctx, tx, "llm_token_totals", "token", side, provider, model, stats.tokenizer, stats.values, now); err != nil {
			return err
		}
		if err := upsertTextTotals(ctx, tx, "llm_word_totals", "word", side, provider, model, "", stats.words, now); err != nil {
			return err
		}
		if err := upsertTextTotals(ctx, tx, "llm_char_totals", "char", side, provider, model, "", stats.chars, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func upsertTextTotals(ctx context.Context, tx *sql.Tx, table, prefix, side, provider, model, tokenizer string, values map[string]textAggregate, now string) error {
	if len(values) == 0 {
		return nil
	}
	var stmt *sql.Stmt
	var err error
	switch table {
	case "llm_token_totals":
		stmt, err = tx.PrepareContext(ctx, `
INSERT INTO llm_token_totals (
  side, provider, model, tokenizer, token_value, token_hash, occurrences, first_seen_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(side, provider, model, tokenizer, token_hash) DO UPDATE SET
  token_value = CASE
    WHEN llm_token_totals.token_value = '' THEN excluded.token_value
    ELSE llm_token_totals.token_value
  END,
  occurrences = llm_token_totals.occurrences + excluded.occurrences,
  updated_at = excluded.updated_at`)
	case "llm_word_totals":
		stmt, err = tx.PrepareContext(ctx, `
INSERT INTO llm_word_totals (
  side, provider, model, word_value, word_hash, occurrences, first_seen_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(side, provider, model, word_hash) DO UPDATE SET
  word_value = CASE
    WHEN llm_word_totals.word_value = '' THEN excluded.word_value
    ELSE llm_word_totals.word_value
  END,
  occurrences = llm_word_totals.occurrences + excluded.occurrences,
  updated_at = excluded.updated_at`)
	case "llm_char_totals":
		stmt, err = tx.PrepareContext(ctx, `
INSERT INTO llm_char_totals (
  side, provider, model, char_value, char_hash, occurrences, first_seen_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(side, provider, model, char_hash) DO UPDATE SET
  char_value = CASE
    WHEN llm_char_totals.char_value = '' THEN excluded.char_value
    ELSE llm_char_totals.char_value
  END,
  occurrences = llm_char_totals.occurrences + excluded.occurrences,
  updated_at = excluded.updated_at`)
	default:
		return fmt.Errorf("unknown aggregate table %q", table)
	}
	if err != nil {
		return err
	}
	defer stmt.Close()

	for valueHash, agg := range values {
		if table == "llm_token_totals" {
			if _, err := stmt.ExecContext(ctx, side, provider, model, tokenizer, agg.value, valueHash, agg.occurrences, now, now); err != nil {
				return err
			}
			continue
		}
		if _, err := stmt.ExecContext(ctx, side, provider, model, agg.value, valueHash, agg.occurrences, now, now); err != nil {
			return err
		}
	}
	_ = prefix
	return nil
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
SELECT side,
  COALESCE(SUM(token_count), 0),
  COALESCE(SUM(char_count), 0),
  COALESCE(SUM(word_count), 0)
FROM llm_token_runs
GROUP BY side`)
	if err != nil {
		return TokenTotals{}, err
	}
	defer rows.Close()

	var totals TokenTotals
	for rows.Next() {
		var side string
		var tokens, chars, words int64
		if err := rows.Scan(&side, &tokens, &chars, &words); err != nil {
			return TokenTotals{}, err
		}
		switch side {
		case "input":
			totals.InputTokens = tokens
			totals.InputChars = chars
			totals.InputWords = words
		case "output":
			totals.OutputTokens = tokens
			totals.OutputChars = chars
			totals.OutputWords = words
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
	topWords, err := s.TopText(ctx, "word", 12)
	if err != nil {
		return TokenTotals{}, err
	}
	totals.TopWords = topWords
	topChars, err := s.TopText(ctx, "char", 12)
	if err != nil {
		return TokenTotals{}, err
	}
	totals.TopChars = topChars
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
SELECT side, provider, model, token_value, token_hash, occurrences
FROM llm_token_totals
WHERE side = ? AND token_value != ''
ORDER BY occurrences DESC, token_value ASC
LIMIT ?`, side, queryLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TokenBurn
	for rows.Next() {
		var burn TokenBurn
		if err := rows.Scan(&burn.Side, &burn.Provider, &burn.Model, &burn.Token, &burn.TokenHash, &burn.Occurrences); err != nil {
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

func (s *Store) TopText(ctx context.Context, kind string, limit int) (TextBurns, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	input, err := s.topTextForSide(ctx, kind, "input", limit)
	if err != nil {
		return TextBurns{}, err
	}
	output, err := s.topTextForSide(ctx, kind, "output", limit)
	if err != nil {
		return TextBurns{}, err
	}
	return TextBurns{Input: input, Output: output}, nil
}

func (s *Store) topTextForSide(ctx context.Context, kind, side string, limit int) ([]TextBurn, error) {
	table := ""
	valueColumn := ""
	hashColumn := ""
	switch kind {
	case "word":
		table = "llm_word_totals"
		valueColumn = "word_value"
		hashColumn = "word_hash"
	case "char":
		table = "llm_char_totals"
		valueColumn = "char_value"
		hashColumn = "char_hash"
	default:
		return nil, fmt.Errorf("unknown text aggregate kind %q", kind)
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT side, provider, model, %s, %s, occurrences
FROM %s
WHERE side = ? AND %s != ''
ORDER BY occurrences DESC, %s ASC
LIMIT ?`, valueColumn, hashColumn, table, valueColumn, valueColumn), side, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TextBurn
	for rows.Next() {
		var burn TextBurn
		if err := rows.Scan(&burn.Side, &burn.Provider, &burn.Model, &burn.Value, &burn.ValueHash, &burn.Occurrences); err != nil {
			return nil, err
		}
		out = append(out, burn)
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
SELECT exchange_id, side, provider, model, tokenizer, count_source,
  token_count, unique_token_count, word_count, unique_word_count,
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
		&run.Provider,
		&run.Model,
		&run.Tokenizer,
		&run.CountSource,
		&run.TokenCount,
		&run.UniqueTokenCount,
		&run.WordCount,
		&run.UniqueWordCount,
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
