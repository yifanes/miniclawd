package storage

import "database/sql"

// AuthAPIKeyRecord represents an API key with its scopes.
type AuthAPIKeyRecord struct {
	ID               int64
	Label            string
	Prefix           string
	CreatedAt        string
	RevokedAt        *string
	ExpiresAt        *string
	LastUsedAt       *string
	RotatedFromKeyID *int64
	Scopes           []string
}

// UpsertAuthPasswordHash sets the master password hash.
func (d *Database) UpsertAuthPasswordHash(hash string) error {
	now := nowRFC3339()
	_, err := d.exec(
		`INSERT INTO auth_passwords (id, password_hash, created_at, updated_at)
		 VALUES (1, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET password_hash = excluded.password_hash, updated_at = excluded.updated_at`,
		hash, now, now,
	)
	return err
}

// GetAuthPasswordHash returns the master password hash.
func (d *Database) GetAuthPasswordHash() (string, bool, error) {
	var hash string
	err := d.queryRow(`SELECT password_hash FROM auth_passwords WHERE id = 1`).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	return hash, err == nil, err
}

// CreateAuthSession creates a new web session.
func (d *Database) CreateAuthSession(sessionID, label, expiresAt string) error {
	now := nowRFC3339()
	_, err := d.exec(
		`INSERT INTO auth_sessions (session_id, label, created_at, expires_at, last_seen_at)
		 VALUES (?, ?, ?, ?, ?)`,
		sessionID, label, now, expiresAt, now,
	)
	return err
}

// ValidateAuthSession checks if a session is valid and updates last_seen.
func (d *Database) ValidateAuthSession(sessionID string) (bool, error) {
	now := nowRFC3339()
	var expiresAt string
	var revokedAt sql.NullString
	err := d.queryRow(
		`SELECT expires_at, revoked_at FROM auth_sessions WHERE session_id = ?`, sessionID,
	).Scan(&expiresAt, &revokedAt)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if revokedAt.Valid {
		return false, nil
	}
	if expiresAt < now {
		return false, nil
	}
	// Update last_seen.
	d.exec(`UPDATE auth_sessions SET last_seen_at = ? WHERE session_id = ?`, now, sessionID)
	return true, nil
}

// RevokeAuthSession revokes a session.
func (d *Database) RevokeAuthSession(sessionID string) error {
	_, err := d.exec(
		`UPDATE auth_sessions SET revoked_at = ? WHERE session_id = ?`,
		nowRFC3339(), sessionID,
	)
	return err
}

// CreateAPIKey creates a new API key record with scopes.
func (d *Database) CreateAPIKey(label, keyHash, prefix string, scopes []string, expiresAt *string, rotatedFromID *int64) (int64, error) {
	var id int64
	err := d.execTx(func(tx *sql.Tx) error {
		result, e := tx.Exec(
			`INSERT INTO api_keys (label, key_hash, prefix, created_at, expires_at, rotated_from_key_id)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			label, keyHash, prefix, nowRFC3339(), expiresAt, rotatedFromID,
		)
		if e != nil {
			return e
		}
		id, e = result.LastInsertId()
		if e != nil {
			return e
		}
		for _, scope := range scopes {
			if _, e = tx.Exec(
				`INSERT INTO api_key_scopes (api_key_id, scope) VALUES (?, ?)`, id, scope,
			); e != nil {
				return e
			}
		}
		return nil
	})
	return id, err
}

// ListAPIKeys returns all API keys with their scopes.
func (d *Database) ListAPIKeys() ([]AuthAPIKeyRecord, error) {
	rows, err := d.query(
		`SELECT id, label, prefix, created_at, revoked_at, expires_at, last_used_at, rotated_from_key_id
		 FROM api_keys ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []AuthAPIKeyRecord
	for rows.Next() {
		var k AuthAPIKeyRecord
		if err := rows.Scan(&k.ID, &k.Label, &k.Prefix, &k.CreatedAt,
			&k.RevokedAt, &k.ExpiresAt, &k.LastUsedAt, &k.RotatedFromKeyID); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load scopes for each key.
	for i := range keys {
		scopeRows, err := d.query(
			`SELECT scope FROM api_key_scopes WHERE api_key_id = ?`, keys[i].ID,
		)
		if err != nil {
			return nil, err
		}
		for scopeRows.Next() {
			var s string
			if err := scopeRows.Scan(&s); err != nil {
				scopeRows.Close()
				return nil, err
			}
			keys[i].Scopes = append(keys[i].Scopes, s)
		}
		scopeRows.Close()
	}
	return keys, nil
}

// ValidateAPIKeyHash checks if an API key hash is valid and updates last_used.
func (d *Database) ValidateAPIKeyHash(keyHash string) (int64, []string, bool, error) {
	var id int64
	var revokedAt, expiresAt sql.NullString
	err := d.queryRow(
		`SELECT id, revoked_at, expires_at FROM api_keys WHERE key_hash = ?`, keyHash,
	).Scan(&id, &revokedAt, &expiresAt)
	if err == sql.ErrNoRows {
		return 0, nil, false, nil
	}
	if err != nil {
		return 0, nil, false, err
	}
	if revokedAt.Valid {
		return 0, nil, false, nil
	}
	now := nowRFC3339()
	if expiresAt.Valid && expiresAt.String < now {
		return 0, nil, false, nil
	}

	// Update last_used.
	d.exec(`UPDATE api_keys SET last_used_at = ? WHERE id = ?`, now, id)

	// Load scopes.
	scopeRows, err := d.query(`SELECT scope FROM api_key_scopes WHERE api_key_id = ?`, id)
	if err != nil {
		return 0, nil, false, err
	}
	defer scopeRows.Close()

	var scopes []string
	for scopeRows.Next() {
		var s string
		if err := scopeRows.Scan(&s); err != nil {
			return 0, nil, false, err
		}
		scopes = append(scopes, s)
	}
	return id, scopes, true, scopeRows.Err()
}

// RevokeAPIKey revokes an API key.
func (d *Database) RevokeAPIKey(id int64) error {
	_, err := d.exec(
		`UPDATE api_keys SET revoked_at = ? WHERE id = ?`,
		nowRFC3339(), id,
	)
	return err
}
