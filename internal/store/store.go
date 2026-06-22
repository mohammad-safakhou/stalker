package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	_ "modernc.org/sqlite"
)

const previewLimit = 64 * 1024

type Store struct {
	db      *sql.DB
	dir     string
	bodyDir string
}

type Exchange struct {
	ID               string    `json:"id"`
	StartedAt        time.Time `json:"started_at"`
	CompletedAt      time.Time `json:"completed_at"`
	DurationMS       int64     `json:"duration_ms"`
	Method           string    `json:"method"`
	Path             string    `json:"path"`
	Query            string    `json:"query"`
	Route            string    `json:"route"`
	TargetURL        string    `json:"target_url"`
	ChatGPTAuth      bool      `json:"chatgpt_auth"`
	StatusCode       int       `json:"status_code"`
	IsStream         bool      `json:"is_stream"`
	Error            string    `json:"error"`
	RequestHeaders   string    `json:"request_headers"`
	ResponseHeaders  string    `json:"response_headers"`
	RequestBodyPath  string    `json:"request_body_path"`
	ResponseBodyPath string    `json:"response_body_path"`
	RequestPreview   string    `json:"request_preview"`
	ResponsePreview  string    `json:"response_preview"`
	RequestBytes     int64     `json:"request_bytes"`
	ResponseBytes    int64     `json:"response_bytes"`
}

type TokenRun struct {
	ExchangeID       string    `json:"exchange_id"`
	Side             string    `json:"side"`
	Tokenizer        string    `json:"tokenizer"`
	TokenCount       int       `json:"token_count"`
	UniqueTokenCount int       `json:"unique_token_count"`
	ByteCount        int64     `json:"byte_count"`
	CharCount        int64     `json:"char_count"`
	DigestSHA256     string    `json:"digest_sha256"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type TokenValue struct {
	ExchangeID    string `json:"exchange_id"`
	Side          string `json:"side"`
	Tokenizer     string `json:"tokenizer"`
	Token         string `json:"token"`
	TokenHash     string `json:"token_hash"`
	Occurrences   int    `json:"occurrences"`
	FirstPosition int    `json:"first_position"`
}

type TokenBurn struct {
	Side        string `json:"side"`
	Token       string `json:"token"`
	TokenHash   string `json:"token_hash"`
	Occurrences int64  `json:"occurrences"`
}

type TokenBurns struct {
	Input  []TokenBurn `json:"input"`
	Output []TokenBurn `json:"output"`
}

type TokenReport struct {
	Runs   []TokenRun   `json:"runs"`
	Values []TokenValue `json:"values"`
}

type TokenTotals struct {
	InputTokens  int64      `json:"input_tokens"`
	OutputTokens int64      `json:"output_tokens"`
	Top          TokenBurns `json:"top"`
}

type ListFilter struct {
	Limit  int
	Offset int
	Query  string
}

type Capture struct {
	store *Store

	ex Exchange

	mu        sync.Mutex
	reqFile   *os.File
	respFile  *os.File
	reqPrev   preview
	respPrev  preview
	reqBytes  int64
	respBytes int64
	reqEnc    string
	respEnc   string
	lastFlush time.Time
	saved     bool
}

type CaptureMeta struct {
	Method      string
	Path        string
	Query       string
	Route       string
	TargetURL   string
	ChatGPTAuth bool
	Headers     http.Header
}

type BodyInfo struct {
	Path     string
	Preview  string
	Bytes    int64
	Encoding string
}

func Open(dir string) (*Store, error) {
	if dir == "" {
		dir = ".stalker"
	}
	bodyDir := filepath.Join(dir, "bodies")
	if err := os.MkdirAll(bodyDir, 0o755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", filepath.Join(dir, "stalker.sqlite"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	s := &Store{db: db, dir: dir, bodyDir: bodyDir}
	if err := s.init(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) init(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA mmap_size=268435456",
	}
	for _, stmt := range pragmas {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}

	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS exchanges (
  id TEXT PRIMARY KEY,
  started_at TEXT NOT NULL,
  completed_at TEXT NOT NULL,
  duration_ms INTEGER NOT NULL,
  method TEXT NOT NULL,
  path TEXT NOT NULL,
  query TEXT NOT NULL,
  route TEXT NOT NULL,
  target_url TEXT NOT NULL,
  chatgpt_auth INTEGER NOT NULL,
  status_code INTEGER NOT NULL,
  is_stream INTEGER NOT NULL,
  error TEXT NOT NULL,
  request_headers TEXT NOT NULL,
  response_headers TEXT NOT NULL,
  request_body_path TEXT NOT NULL,
  response_body_path TEXT NOT NULL,
  request_preview TEXT NOT NULL,
  response_preview TEXT NOT NULL,
  request_bytes INTEGER NOT NULL,
  response_bytes INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_exchanges_started_at ON exchanges(started_at DESC);
CREATE INDEX IF NOT EXISTS idx_exchanges_path ON exchanges(path);
CREATE INDEX IF NOT EXISTS idx_exchanges_status_code ON exchanges(status_code);

CREATE TABLE IF NOT EXISTS llm_token_runs (
  exchange_id TEXT NOT NULL,
  side TEXT NOT NULL,
  tokenizer TEXT NOT NULL,
  token_count INTEGER NOT NULL,
  unique_token_count INTEGER NOT NULL,
  byte_count INTEGER NOT NULL,
  char_count INTEGER NOT NULL,
  digest_sha256 TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (exchange_id, side, tokenizer)
);
CREATE INDEX IF NOT EXISTS idx_llm_token_runs_updated_at ON llm_token_runs(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_llm_token_runs_side ON llm_token_runs(side);

CREATE TABLE IF NOT EXISTS llm_token_values (
  exchange_id TEXT NOT NULL,
  side TEXT NOT NULL,
  tokenizer TEXT NOT NULL,
  token_value TEXT NOT NULL DEFAULT '',
  token_hash TEXT NOT NULL,
  occurrences INTEGER NOT NULL,
  first_position INTEGER NOT NULL,
  PRIMARY KEY (exchange_id, side, tokenizer, token_hash)
);
CREATE INDEX IF NOT EXISTS idx_llm_token_values_hash ON llm_token_values(token_hash);
`)
	if err != nil {
		return err
	}
	return s.ensureTokenValueColumns(ctx)
}

func (s *Store) ensureTokenValueColumns(ctx context.Context) error {
	hasTokenValue, err := s.hasColumn(ctx, "llm_token_values", "token_value")
	if err != nil {
		return err
	}
	if !hasTokenValue {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE llm_token_values ADD COLUMN token_value TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) hasColumn(ctx context.Context, table, column string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var cid int
		var name, dataType string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if strings.EqualFold(name, column) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) NewCapture(meta CaptureMeta) (*Capture, error) {
	id := newID()
	reqFile, reqPath, err := s.createBodyFile(id, "request")
	if err != nil {
		return nil, err
	}
	respFile, respPath, err := s.createBodyFile(id, "response")
	if err != nil {
		reqFile.Close()
		return nil, err
	}

	capture := &Capture{
		store:     s,
		reqFile:   reqFile,
		respFile:  respFile,
		reqEnc:    contentEncoding(meta.Headers),
		lastFlush: time.Now().UTC(),
		ex: Exchange{
			ID:               id,
			StartedAt:        time.Now().UTC(),
			Method:           meta.Method,
			Path:             meta.Path,
			Query:            meta.Query,
			Route:            meta.Route,
			TargetURL:        meta.TargetURL,
			ChatGPTAuth:      meta.ChatGPTAuth,
			RequestHeaders:   marshalHeaders(meta.Headers),
			RequestBodyPath:  reqPath,
			ResponseBodyPath: respPath,
		},
	}
	if err := capture.Flush(""); err != nil {
		_ = reqFile.Close()
		_ = respFile.Close()
		return nil, err
	}
	return capture, nil
}

func (s *Store) createBodyFile(id, kind string) (*os.File, string, error) {
	date := time.Now().UTC().Format("2006-01-02")
	dir := filepath.Join(s.bodyDir, date)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, id+"."+kind+".body")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "", err
	}
	return f, path, nil
}

func (c *Capture) RequestBody(body io.ReadCloser) io.ReadCloser {
	if body == nil {
		return nil
	}
	return &captureReadCloser{
		ReadCloser: body,
		write: func(chunk []byte) {
			c.WriteRequest(chunk)
		},
		close: func() {
			c.mu.Lock()
			defer c.mu.Unlock()
			_ = c.reqFile.Close()
		},
	}
}

func (c *Capture) ResponseBody(body io.ReadCloser, statusCode int, headers http.Header, isStream bool) io.ReadCloser {
	if body == nil {
		return nil
	}
	c.ResponseMeta(statusCode, headers, isStream)
	_ = c.Flush("")

	return &captureReadCloser{
		ReadCloser: body,
		write: func(chunk []byte) {
			c.WriteResponse(chunk)
		},
		close: func() {
			c.mu.Lock()
			_ = c.respFile.Close()
			c.mu.Unlock()
			_ = c.Save("")
		},
	}
}

func (c *Capture) WriteRequest(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.saved {
		return
	}
	c.reqBytes += int64(len(chunk))
	c.reqPrev.Write(chunk)
	_, _ = c.reqFile.Write(chunk)
}

func (c *Capture) WriteResponse(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.saved {
		return
	}
	c.respBytes += int64(len(chunk))
	c.respPrev.Write(chunk)
	_, _ = c.respFile.Write(chunk)
}

func (c *Capture) ResponseMeta(statusCode int, headers http.Header, isStream bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.saved {
		return
	}
	c.ex.StatusCode = statusCode
	c.ex.ResponseHeaders = marshalHeaders(headers)
	c.ex.IsStream = isStream
	c.respEnc = contentEncoding(headers)
}

func (c *Capture) Flush(errorMessage string) error {
	c.mu.Lock()
	if c.saved {
		c.mu.Unlock()
		return nil
	}
	c.lastFlush = time.Now().UTC()
	ex := c.snapshotLocked(errorMessage, false)
	c.mu.Unlock()

	ctx := context.Background()
	if err := c.store.Insert(ctx, ex); err != nil {
		return err
	}
	return c.store.ProcessExchangeTokens(ctx, ex, false)
}

func (c *Capture) Save(errorMessage string) error {
	c.mu.Lock()
	if c.saved {
		c.mu.Unlock()
		return nil
	}
	c.saved = true
	_ = c.reqFile.Close()
	_ = c.respFile.Close()

	c.lastFlush = time.Now().UTC()
	ex := c.snapshotLocked(errorMessage, true)
	c.mu.Unlock()

	ctx := context.Background()
	if err := c.store.Insert(ctx, ex); err != nil {
		return err
	}
	return c.store.ProcessExchangeTokens(ctx, ex, true)
}

func (c *Capture) snapshotLocked(errorMessage string, final bool) Exchange {
	c.ex.CompletedAt = time.Now().UTC()
	c.ex.DurationMS = c.ex.CompletedAt.Sub(c.ex.StartedAt).Milliseconds()
	c.ex.Error = errorMessage
	c.ex.RequestBytes = c.reqBytes
	c.ex.ResponseBytes = c.respBytes
	c.ex.RequestPreview = c.reqPrev.String()
	c.ex.ResponsePreview = c.respPrev.String()
	if final {
		c.ex.RequestPreview = previewFromFile(c.ex.RequestBodyPath, c.reqEnc, c.ex.RequestPreview)
		c.ex.ResponsePreview = previewFromFile(c.ex.ResponseBodyPath, c.respEnc, c.ex.ResponsePreview)
	}
	return c.ex
}

func (s *Store) Insert(ctx context.Context, ex Exchange) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO exchanges (
  id, started_at, completed_at, duration_ms, method, path, query, route,
  target_url, chatgpt_auth, status_code, is_stream, error, request_headers,
  response_headers, request_body_path, response_body_path, request_preview,
  response_preview, request_bytes, response_bytes
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
	  completed_at = excluded.completed_at,
	  duration_ms = excluded.duration_ms,
	  status_code = excluded.status_code,
	  is_stream = excluded.is_stream,
	  error = excluded.error,
	  response_headers = excluded.response_headers,
	  request_preview = excluded.request_preview,
	  response_preview = excluded.response_preview,
	  request_bytes = excluded.request_bytes,
	  response_bytes = excluded.response_bytes`,
		ex.ID,
		ex.StartedAt.Format(time.RFC3339Nano),
		ex.CompletedAt.Format(time.RFC3339Nano),
		ex.DurationMS,
		ex.Method,
		ex.Path,
		ex.Query,
		ex.Route,
		ex.TargetURL,
		boolInt(ex.ChatGPTAuth),
		ex.StatusCode,
		boolInt(ex.IsStream),
		ex.Error,
		ex.RequestHeaders,
		ex.ResponseHeaders,
		ex.RequestBodyPath,
		ex.ResponseBodyPath,
		ex.RequestPreview,
		ex.ResponsePreview,
		ex.RequestBytes,
		ex.ResponseBytes,
	)
	return err
}

func (s *Store) List(ctx context.Context, filter ListFilter) ([]Exchange, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	args := []any{}
	where := ""
	if q := strings.TrimSpace(filter.Query); q != "" {
		where = `WHERE path LIKE ? OR route LIKE ? OR target_url LIKE ? OR request_preview LIKE ? OR response_preview LIKE ?`
		like := "%" + q + "%"
		args = append(args, like, like, like, like, like)
	}
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, `
SELECT id, started_at, completed_at, duration_ms, method, path, query, route,
  target_url, chatgpt_auth, status_code, is_stream, error, request_headers,
  response_headers, request_body_path, response_body_path, request_preview,
  response_preview, request_bytes, response_bytes
FROM exchanges
`+where+`
ORDER BY started_at DESC
LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Exchange{}
	for rows.Next() {
		ex, err := scanExchange(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ex)
	}
	return out, rows.Err()
}

func (s *Store) Get(ctx context.Context, id string) (Exchange, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, started_at, completed_at, duration_ms, method, path, query, route,
  target_url, chatgpt_auth, status_code, is_stream, error, request_headers,
  response_headers, request_body_path, response_body_path, request_preview,
  response_preview, request_bytes, response_bytes
FROM exchanges
WHERE id = ?`, id)
	return scanExchange(row)
}

func (s *Store) Body(ctx context.Context, id, side string) (BodyInfo, error) {
	ex, err := s.Get(ctx, id)
	if err != nil {
		return BodyInfo{}, err
	}
	switch side {
	case "request":
		return BodyInfo{
			Path:     ex.RequestBodyPath,
			Preview:  ex.RequestPreview,
			Bytes:    ex.RequestBytes,
			Encoding: contentEncodingFromJSON(ex.RequestHeaders),
		}, nil
	case "response":
		return BodyInfo{
			Path:     ex.ResponseBodyPath,
			Preview:  ex.ResponsePreview,
			Bytes:    ex.ResponseBytes,
			Encoding: contentEncodingFromJSON(ex.ResponseHeaders),
		}, nil
	default:
		return BodyInfo{}, fmt.Errorf("unknown body side %q", side)
	}
}

func (s *Store) OpenBody(info BodyInfo) (io.ReadCloser, error) {
	return openDecodedFile(info.Path, info.Encoding)
}

type exchangeScanner interface {
	Scan(dest ...any) error
}

func scanExchange(scanner exchangeScanner) (Exchange, error) {
	var ex Exchange
	var startedAt, completedAt string
	var chatGPTAuth, isStream int
	err := scanner.Scan(
		&ex.ID,
		&startedAt,
		&completedAt,
		&ex.DurationMS,
		&ex.Method,
		&ex.Path,
		&ex.Query,
		&ex.Route,
		&ex.TargetURL,
		&chatGPTAuth,
		&ex.StatusCode,
		&isStream,
		&ex.Error,
		&ex.RequestHeaders,
		&ex.ResponseHeaders,
		&ex.RequestBodyPath,
		&ex.ResponseBodyPath,
		&ex.RequestPreview,
		&ex.ResponsePreview,
		&ex.RequestBytes,
		&ex.ResponseBytes,
	)
	if err != nil {
		return Exchange{}, err
	}
	ex.StartedAt, _ = time.Parse(time.RFC3339Nano, startedAt)
	ex.CompletedAt, _ = time.Parse(time.RFC3339Nano, completedAt)
	ex.ChatGPTAuth = chatGPTAuth != 0
	ex.IsStream = isStream != 0
	ex.RequestPreview = decodedPreviewIfEncoded(ex.RequestBodyPath, ex.RequestHeaders, ex.RequestPreview)
	ex.ResponsePreview = decodedPreviewIfEncoded(ex.ResponseBodyPath, ex.ResponseHeaders, ex.ResponsePreview)
	return ex, nil
}

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}

func marshalHeaders(headers http.Header) string {
	if len(headers) == 0 {
		return "{}"
	}
	clean := make(map[string][]string, len(headers))
	for key, values := range headers {
		switch strings.ToLower(key) {
		case "authorization", "cookie", "set-cookie":
			clean[key] = []string{"[redacted]"}
		default:
			clean[key] = values
		}
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func contentEncoding(headers http.Header) string {
	return strings.ToLower(strings.TrimSpace(headers.Get("Content-Encoding")))
}

func contentEncodingFromJSON(raw string) string {
	var headers map[string][]string
	if err := json.Unmarshal([]byte(raw), &headers); err != nil {
		return ""
	}
	for key, values := range headers {
		if strings.EqualFold(key, "Content-Encoding") && len(values) > 0 {
			return strings.ToLower(strings.TrimSpace(values[0]))
		}
	}
	return ""
}

func previewFromFile(path, encoding, fallback string) string {
	r, err := openDecodedFile(path, encoding)
	if err != nil {
		if encoding != "" && fallback != "" {
			return fmt.Sprintf("[body is %s encoded; preview decode failed: %v]\n%s", encoding, err, fallback)
		}
		return fallback
	}
	defer r.Close()

	var buf bytes.Buffer
	if _, err := io.CopyN(&buf, r, previewLimit); err != nil && !errors.Is(err, io.EOF) {
		if fallback != "" {
			return fallback
		}
	}
	return buf.String()
}

func decodedPreviewIfEncoded(path, headersJSON, fallback string) string {
	encoding := contentEncodingFromJSON(headersJSON)
	if encoding == "" || encoding == "identity" {
		return fallback
	}
	return previewFromFile(path, encoding, fallback)
}

func openDecodedFile(path, encoding string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "identity":
		return f, nil
	case "gzip":
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return &compoundReadCloser{Reader: gz, closers: []io.Closer{gz, f}}, nil
	case "zstd":
		decoder, err := zstd.NewReader(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return &compoundReadCloser{
			Reader:  decoder,
			closers: []io.Closer{closerFunc(func() error { decoder.Close(); return nil }), f},
		}, nil
	default:
		return f, nil
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func newID() string {
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), randomSuffix())
}

func randomSuffix() string {
	var b [8]byte
	f, err := os.Open("/dev/urandom")
	if err == nil {
		defer f.Close()
		if _, err := io.ReadFull(f, b[:]); err == nil {
			return fmt.Sprintf("%x", b[:])
		}
	}
	return fmt.Sprintf("%08x", time.Now().Nanosecond())
}

type preview struct {
	buf bytes.Buffer
}

func (p *preview) Write(chunk []byte) {
	remaining := previewLimit - p.buf.Len()
	if remaining <= 0 {
		return
	}
	if len(chunk) > remaining {
		chunk = chunk[:remaining]
	}
	p.buf.Write(chunk)
}

func (p *preview) String() string {
	return p.buf.String()
}

type captureReadCloser struct {
	io.ReadCloser
	write func([]byte)
	close func()
}

type compoundReadCloser struct {
	io.Reader
	closers []io.Closer
}

type closerFunc func() error

func (f closerFunc) Close() error {
	return f()
}

func (c *compoundReadCloser) Close() error {
	var first error
	for _, closer := range c.closers {
		if err := closer.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (c *captureReadCloser) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	if n > 0 && c.write != nil {
		c.write(p[:n])
	}
	return n, err
}

func (c *captureReadCloser) Close() error {
	err := c.ReadCloser.Close()
	if c.close != nil {
		c.close()
	}
	return err
}
