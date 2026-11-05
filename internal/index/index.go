// Package index keeps a search index of the case folder texts in SQLite:
// full text search (FTS5, accents folded) and embeddings from a local model.
// The index is a cache: deleting extracted/index.sqlite only costs a rebuild.
package index

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	// Registers the "sqlite" database/sql driver (pure Go).
	_ "modernc.org/sqlite"

	"github.com/AlfrescoLabs/openarch-docs/internal/casefile"
)

// File is the index path, relative to the case folder.
const File = casefile.ExtractedDir + "/index.sqlite"

// queryPrefix is the instruction mxbai-embed-large wants before a query.
const queryPrefix = "Represent this sentence for searching relevant passages: "

// embedBatch is the number of chunks per embeddings request.
const embedBatch = 32

// Embedder makes normalized embeddings.
type Embedder interface {
	Embed(ctx context.Context, model string, inputs []string) ([][]float32, error)
}

const schema = `
CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS files(path TEXT PRIMARY KEY, sha256 TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS chunks(
  id INTEGER PRIMARY KEY,
  path TEXT NOT NULL, source TEXT NOT NULL, origin TEXT NOT NULL, page INTEGER NOT NULL,
  kind TEXT NOT NULL, role TEXT NOT NULL, method TEXT NOT NULL, cite TEXT NOT NULL,
  date TEXT NOT NULL, sender TEXT NOT NULL, text TEXT NOT NULL, embedding BLOB);
CREATE INDEX IF NOT EXISTS chunks_path ON chunks(path);
CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(text, tokenize='unicode61 remove_diacritics 2');
`

// Index is an open index.
type Index struct {
	db *sql.DB
}

// Open opens or creates the index of the case folder root.
func Open(ctx context.Context, root string) (*Index, error) {
	db, err := sql.Open("sqlite", filepath.Join(root, filepath.FromSlash(File)))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s: %w", File, err)
	}
	return &Index{db: db}, nil
}

// Close closes the index.
func (x *Index) Close() error { return x.db.Close() }

// BuildOptions configure an index update.
type BuildOptions struct {
	Root string
	// Embedder and Model make the embeddings. With no Embedder, only full
	// text search is updated; a later run with one fills the embeddings.
	Embedder Embedder
	Model    string
	// Progress receives a line per batch. It may be nil.
	Progress io.Writer
}

// BuildResult is the outcome of an index update.
type BuildResult struct {
	Files     int    `json:"files"`
	Updated   int    `json:"updated"`
	Removed   int    `json:"removed"`
	Chunks    int    `json:"chunks"`
	Embedded  int    `json:"embedded"`
	Pending   int    `json:"pendingEmbeddings"`
	Model     string `json:"model,omitempty"`
	IndexFile string `json:"index"`
}

// Build updates the index from the manifest and the notes: new and changed
// texts are split again, removed ones are dropped, and chunks without
// embeddings get them.
func (x *Index) Build(ctx context.Context, opts BuildOptions) (*BuildResult, error) {
	m, err := casefile.Load(opts.Root)
	if err != nil {
		return nil, err
	}
	docs, err := collect(opts.Root, m)
	if err != nil {
		return nil, err
	}
	res := &BuildResult{Files: len(docs), Model: opts.Model, IndexFile: File}
	if opts.Embedder != nil {
		// Embeddings of another model cannot be compared with new ones.
		old, err := x.meta(ctx, "embed_model")
		if err != nil {
			return nil, err
		}
		if old != "" && old != opts.Model {
			if _, err := x.db.ExecContext(ctx, `UPDATE chunks SET embedding = NULL`); err != nil {
				return nil, err
			}
		}
		if err := x.setMeta(ctx, "embed_model", opts.Model); err != nil {
			return nil, err
		}
	}

	known := map[string]string{}
	rows, err := x.db.QueryContext(ctx, `SELECT path, sha256 FROM files`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p, sha string
		if err := rows.Scan(&p, &sha); err != nil {
			_ = rows.Close()
			return nil, err
		}
		known[p] = sha
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	current := map[string]bool{}
	for _, d := range docs {
		current[d.path] = true
		if known[d.path] == d.sha {
			continue
		}
		text, err := readText(opts.Root, d)
		if err != nil {
			return nil, err
		}
		if err := x.replace(ctx, d, chunks(text)); err != nil {
			return nil, err
		}
		res.Updated++
	}
	for p := range known {
		if !current[p] {
			if err := x.remove(ctx, p); err != nil {
				return nil, err
			}
			res.Removed++
		}
	}
	if opts.Embedder != nil {
		n, err := x.embedMissing(ctx, opts)
		res.Embedded = n
		if err != nil {
			return res, err
		}
	}
	if err := x.db.QueryRowContext(ctx, `SELECT count(*), count(*) - count(embedding) FROM chunks`).Scan(&res.Chunks, &res.Pending); err != nil {
		return nil, err
	}
	return res, nil
}

func (x *Index) replace(ctx context.Context, d doc, pieces []string) error {
	tx, err := x.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := removeTx(ctx, tx, d.path); err != nil {
		return err
	}
	for _, text := range pieces {
		r, err := tx.ExecContext(ctx, `INSERT INTO chunks(path, source, origin, page, kind, role, method, cite, date, sender, text) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			d.path, d.source, d.origin, d.page, d.kind, d.role, d.method, d.cite, d.date, d.sender, text)
		if err != nil {
			return err
		}
		id, err := r.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO chunks_fts(rowid, text) VALUES (?, ?)`, id, text); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO files(path, sha256) VALUES (?, ?) ON CONFLICT(path) DO UPDATE SET sha256 = excluded.sha256`, d.path, d.sha); err != nil {
		return err
	}
	return tx.Commit()
}

func (x *Index) remove(ctx context.Context, p string) error {
	tx, err := x.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := removeTx(ctx, tx, p); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM files WHERE path = ?`, p); err != nil {
		return err
	}
	return tx.Commit()
}

func removeTx(ctx context.Context, tx *sql.Tx, p string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM chunks_fts WHERE rowid IN (SELECT id FROM chunks WHERE path = ?)`, p); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM chunks WHERE path = ?`, p)
	return err
}

// embedMissing embeds the chunks that have no embedding yet.
func (x *Index) embedMissing(ctx context.Context, opts BuildOptions) (int, error) {
	done := 0
	for {
		rows, err := x.db.QueryContext(ctx, `SELECT id, cite, text FROM chunks WHERE embedding IS NULL ORDER BY id LIMIT ?`, embedBatch)
		if err != nil {
			return done, err
		}
		var ids []int64
		var inputs []string
		for rows.Next() {
			var id int64
			var cite, text string
			if err := rows.Scan(&id, &cite, &text); err != nil {
				_ = rows.Close()
				return done, err
			}
			ids = append(ids, id)
			inputs = append(inputs, cite+"\n"+text)
		}
		if err := rows.Close(); err != nil {
			return done, err
		}
		if len(ids) == 0 {
			return done, nil
		}
		vecs, err := opts.Embedder.Embed(ctx, opts.Model, inputs)
		if err != nil {
			return done, err
		}
		tx, err := x.db.BeginTx(ctx, nil)
		if err != nil {
			return done, err
		}
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE chunks SET embedding = ? WHERE id = ?`, encode(vecs[i]), id); err != nil {
				_ = tx.Rollback()
				return done, err
			}
		}
		if err := tx.Commit(); err != nil {
			return done, err
		}
		done += len(ids)
		if opts.Progress != nil {
			_, _ = fmt.Fprintf(opts.Progress, "Embedded %d chunks\n", done)
		}
	}
}

// SearchOptions configure a search.
type SearchOptions struct {
	Query string
	Limit int
	// Roles keeps only chunks with these roles: case, reference, note. Empty keeps all.
	Roles []string
	// Embedder and Model add semantic search. With no Embedder, only full text search runs.
	Embedder Embedder
	Model    string
}

// Hit is one search result.
type Hit struct {
	Rank   int     `json:"rank"`
	Score  float64 `json:"score"`
	Cite   string  `json:"cite"`
	Path   string  `json:"path"`
	Source string  `json:"source"`
	Origin string  `json:"origin"`
	Page   int     `json:"page,omitempty"`
	Kind   string  `json:"kind"`
	Role   string  `json:"role"`
	Method string  `json:"method,omitempty"`
	Date   string  `json:"date,omitempty"`
	Sender string  `json:"sender,omitempty"`
	Text   string  `json:"text"`
	// Matched tells which searches found the chunk: "text", "meaning" or both.
	Matched []string `json:"matched"`
}

// candidates is how many results each search gives to the fusion.
const candidates = 50

// rrfK is the constant of reciprocal rank fusion.
const rrfK = 60

// Search runs full text search and, with an Embedder, semantic search, and
// merges the two lists with reciprocal rank fusion.
func (x *Index) Search(ctx context.Context, opts SearchOptions) ([]Hit, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	scores := map[int64]float64{}
	matched := map[int64][]string{}
	ftsIDs, err := x.searchText(ctx, opts.Query)
	if err != nil {
		return nil, err
	}
	for i, id := range ftsIDs {
		scores[id] += 1.0 / float64(rrfK+i+1)
		matched[id] = append(matched[id], "text")
	}
	if opts.Embedder != nil {
		vecIDs, err := x.searchMeaning(ctx, opts)
		if err != nil {
			return nil, err
		}
		for i, id := range vecIDs {
			scores[id] += 1.0 / float64(rrfK+i+1)
			matched[id] = append(matched[id], "meaning")
		}
	}
	ids := make([]int64, 0, len(scores))
	for id := range scores {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b int64) int {
		switch {
		case scores[a] > scores[b]:
			return -1
		case scores[a] < scores[b]:
			return 1
		}
		return int(a - b)
	})
	var hits []Hit
	for _, id := range ids {
		h, err := x.hit(ctx, id)
		if err != nil {
			return nil, err
		}
		if len(opts.Roles) > 0 && !slices.Contains(opts.Roles, h.Role) {
			continue
		}
		h.Rank, h.Score, h.Matched = len(hits)+1, math.Round(scores[id]*1e5)/1e5, matched[id]
		hits = append(hits, h)
		if len(hits) == opts.Limit {
			break
		}
	}
	return hits, nil
}

func (x *Index) hit(ctx context.Context, id int64) (Hit, error) {
	var h Hit
	err := x.db.QueryRowContext(ctx, `SELECT cite, path, source, origin, page, kind, role, method, date, sender, text FROM chunks WHERE id = ?`, id).
		Scan(&h.Cite, &h.Path, &h.Source, &h.Origin, &h.Page, &h.Kind, &h.Role, &h.Method, &h.Date, &h.Sender, &h.Text)
	return h, err
}

// searchText returns chunk ids by BM25 rank. Each word of the query is a term,
// and any term can match.
func (x *Index) searchText(ctx context.Context, query string) ([]int64, error) {
	words := strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	var terms []string
	for _, w := range words {
		if len([]rune(w)) >= 2 {
			terms = append(terms, `"`+w+`"`)
		}
	}
	if len(terms) == 0 {
		return nil, nil
	}
	rows, err := x.db.QueryContext(ctx, `SELECT rowid FROM chunks_fts WHERE chunks_fts MATCH ? ORDER BY bm25(chunks_fts) LIMIT ?`,
		strings.Join(terms, " OR "), candidates)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// searchMeaning returns chunk ids by cosine similarity to the query.
func (x *Index) searchMeaning(ctx context.Context, opts SearchOptions) ([]int64, error) {
	vecs, err := opts.Embedder.Embed(ctx, opts.Model, []string{queryPrefix + opts.Query})
	if err != nil {
		return nil, err
	}
	q := vecs[0]
	rows, err := x.db.QueryContext(ctx, `SELECT id, embedding FROM chunks WHERE embedding IS NOT NULL`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	type scored struct {
		id  int64
		sim float32
	}
	var all []scored
	for rows.Next() {
		var id int64
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, err
		}
		v := decode(blob)
		if len(v) != len(q) {
			continue
		}
		var dot float32
		for i := range v {
			dot += v[i] * q[i]
		}
		all = append(all, scored{id, dot})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortFunc(all, func(a, b scored) int {
		switch {
		case a.sim > b.sim:
			return -1
		case a.sim < b.sim:
			return 1
		}
		return int(a.id - b.id)
	})
	ids := make([]int64, 0, candidates)
	for i := 0; i < len(all) && i < candidates; i++ {
		ids = append(ids, all[i].id)
	}
	return ids, nil
}

func (x *Index) meta(ctx context.Context, key string) (string, error) {
	var v string
	err := x.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (x *Index) setMeta(ctx context.Context, key, value string) error {
	_, err := x.db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

func encode(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

func decode(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}
