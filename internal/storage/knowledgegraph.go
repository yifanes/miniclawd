package storage

import (
	"fmt"
	"strings"
	"time"
)

// KGTriple represents a subject-predicate-object triple in the knowledge graph.
type KGTriple struct {
	ID        int64
	ChatID    *int64
	Subject   string
	Predicate string
	Object    string
	ValidFrom string
	ValidTo   *string
	Source    string
	CreatedAt string
}

// KGAddTriple adds a triple to the knowledge graph. If a matching
// subject+predicate triple already exists for the same chat, its valid_to
// is set to now (temporal superseding).
func (db *Database) KGAddTriple(chatID *int64, subject, predicate, object, source string) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)

	// Close any existing active triple with the same subject+predicate+chatID.
	if chatID != nil {
		db.db.Exec(`UPDATE kg_triples SET valid_to = ? WHERE subject = ? AND predicate = ? AND chat_id = ? AND valid_to IS NULL`,
			now, subject, predicate, *chatID)
	} else {
		db.db.Exec(`UPDATE kg_triples SET valid_to = ? WHERE subject = ? AND predicate = ? AND chat_id IS NULL AND valid_to IS NULL`,
			now, subject, predicate)
	}

	_, err := db.db.Exec(
		`INSERT INTO kg_triples (chat_id, subject, predicate, object, valid_from, source, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		chatID, subject, predicate, object, now, source, now)
	return err
}

// KGQuerySubject returns active triples for a given subject. If asOf is non-empty,
// returns triples valid at that point in time.
func (db *Database) KGQuerySubject(subject string, chatID *int64, asOf *string) ([]KGTriple, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var args []interface{}
	query := `SELECT id, chat_id, subject, predicate, object, valid_from, valid_to, source, created_at FROM kg_triples WHERE subject = ?`
	args = append(args, subject)

	if chatID != nil {
		query += ` AND (chat_id = ? OR chat_id IS NULL)`
		args = append(args, *chatID)
	}

	if asOf != nil && *asOf != "" {
		query += ` AND valid_from <= ? AND (valid_to IS NULL OR valid_to > ?)`
		args = append(args, *asOf, *asOf)
	} else {
		query += ` AND valid_to IS NULL`
	}

	query += ` ORDER BY valid_from DESC LIMIT 100`
	return db.queryTriples(query, args...)
}

// KGQueryObject returns active triples where the object matches.
func (db *Database) KGQueryObject(object string, chatID *int64) ([]KGTriple, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var args []interface{}
	query := `SELECT id, chat_id, subject, predicate, object, valid_from, valid_to, source, created_at FROM kg_triples WHERE object = ?`
	args = append(args, object)

	if chatID != nil {
		query += ` AND (chat_id = ? OR chat_id IS NULL)`
		args = append(args, *chatID)
	}

	query += ` AND valid_to IS NULL ORDER BY valid_from DESC LIMIT 100`
	return db.queryTriples(query, args...)
}

// KGQueryTimeline returns all historical triples (including superseded) for an entity.
func (db *Database) KGQueryTimeline(entity string, chatID *int64) ([]KGTriple, error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	var args []interface{}
	query := `SELECT id, chat_id, subject, predicate, object, valid_from, valid_to, source, created_at FROM kg_triples WHERE (subject = ? OR object = ?)`
	args = append(args, entity, entity)

	if chatID != nil {
		query += ` AND (chat_id = ? OR chat_id IS NULL)`
		args = append(args, *chatID)
	}

	query += ` ORDER BY valid_from ASC LIMIT 200`
	return db.queryTriples(query, args...)
}

// KGStats returns basic statistics about the knowledge graph.
func (db *Database) KGStats(chatID *int64) (total int, active int, err error) {
	db.mu.Lock()
	defer db.mu.Unlock()

	if chatID != nil {
		db.db.QueryRow(`SELECT COUNT(*) FROM kg_triples WHERE chat_id = ? OR chat_id IS NULL`, *chatID).Scan(&total)
		db.db.QueryRow(`SELECT COUNT(*) FROM kg_triples WHERE (chat_id = ? OR chat_id IS NULL) AND valid_to IS NULL`, *chatID).Scan(&active)
	} else {
		db.db.QueryRow(`SELECT COUNT(*) FROM kg_triples`).Scan(&total)
		db.db.QueryRow(`SELECT COUNT(*) FROM kg_triples WHERE valid_to IS NULL`).Scan(&active)
	}
	return total, active, nil
}

func (db *Database) queryTriples(query string, args ...interface{}) ([]KGTriple, error) {
	rows, err := db.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var triples []KGTriple
	for rows.Next() {
		var t KGTriple
		if err := rows.Scan(&t.ID, &t.ChatID, &t.Subject, &t.Predicate, &t.Object,
			&t.ValidFrom, &t.ValidTo, &t.Source, &t.CreatedAt); err != nil {
			return nil, err
		}
		triples = append(triples, t)
	}
	return triples, rows.Err()
}

// FormatTriples formats triples for display.
func FormatTriples(triples []KGTriple) string {
	if len(triples) == 0 {
		return "No triples found."
	}
	var lines []string
	for _, t := range triples {
		validity := "active"
		if t.ValidTo != nil {
			validity = fmt.Sprintf("superseded at %s", *t.ValidTo)
		}
		lines = append(lines, fmt.Sprintf("- %s → %s → %s (since %s, %s)",
			t.Subject, t.Predicate, t.Object, t.ValidFrom, validity))
	}
	return strings.Join(lines, "\n")
}
