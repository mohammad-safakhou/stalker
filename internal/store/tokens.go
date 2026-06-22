package store

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"
	"unicode"
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
	occurrences   int
	firstPosition int
}

func (s *Store) ProcessExchangeTokens(ctx context.Context, ex Exchange, final bool) error {
	sides := []struct {
		name     string
		path     string
		headers  string
		preview  string
		rawBytes int64
	}{
		{name: "input", path: ex.RequestBodyPath, headers: ex.RequestHeaders, preview: ex.RequestPreview, rawBytes: ex.RequestBytes},
		{name: "output", path: ex.ResponseBodyPath, headers: ex.ResponseHeaders, preview: ex.ResponsePreview, rawBytes: ex.ResponseBytes},
	}

	for _, side := range sides {
		stats, err := tokenizeExchangeSide(side.path, side.headers, side.preview, final)
		if err != nil {
			return err
		}
		if !final && stats.byteCount == 0 && side.rawBytes > 0 {
			stats.byteCount = side.rawBytes
		}
		if err := s.upsertTokenStats(ctx, ex.ID, side.name, stats); err != nil {
			return err
		}
	}
	return nil
}

func tokenizeExchangeSide(path, headersJSON, preview string, final bool) (tokenStats, error) {
	if final && path != "" {
		body, err := openDecodedFile(path, contentEncodingFromJSON(headersJSON))
		if err == nil {
			defer body.Close()
			return tokenizeReader(body)
		}
		if preview == "" && !errors.Is(err, io.EOF) {
			return tokenStats{}, err
		}
	}
	return tokenizeReader(strings.NewReader(preview))
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

func (s *Store) upsertTokenStats(ctx context.Context, exchangeID, side string, stats tokenStats) error {
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

	if _, err := tx.ExecContext(ctx, `
DELETE FROM llm_token_values
WHERE exchange_id = ? AND side = ? AND tokenizer = ?`,
		exchangeID,
		side,
		tokenizerVersion,
	); err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO llm_token_values (
  exchange_id, side, tokenizer, token_hash, occurrences, first_position
) VALUES (?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for tokenHash, agg := range stats.values {
		if _, err := stmt.ExecContext(ctx, exchangeID, side, tokenizerVersion, tokenHash, agg.occurrences, agg.firstPosition); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) TokenReport(ctx context.Context, exchangeID string, limit int) (TokenReport, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}

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
	return totals, rows.Err()
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
	rows, err := s.db.QueryContext(ctx, `
SELECT exchange_id, side, tokenizer, token_hash, occurrences, first_position
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
