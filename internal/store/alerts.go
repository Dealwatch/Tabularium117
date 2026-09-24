package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Dealwatch/Tabularium117/internal/alerts"
	"github.com/Dealwatch/Tabularium117/internal/model"
)

// SQL for the alert table (migration 2).
const (
	// insertAlertSQL opens an alert row. The value that triggered the rule is
	// not stored; see schema2.
	insertAlertSQL = `INSERT INTO alert (island, product_guid, rule, severity, raised_at, cleared_at, detail)
		VALUES (?, ?, ?, ?, ?, NULL, ?)`

	// clearAlertSQL closes the open row of one (island, product, rule). An
	// already closed row is left alone, so a repeated clear cannot move the
	// end time.
	clearAlertSQL = `UPDATE alert SET cleared_at = ?
		WHERE island = ? AND product_guid = ? AND rule = ? AND cleared_at IS NULL`

	// closeOpenAlertsSQL is the restart case: the rule engine's state lives
	// in memory only, so every alert this process did not raise itself is
	// over as far as it can tell.
	closeOpenAlertsSQL = `UPDATE alert SET cleared_at = ? WHERE cleared_at IS NULL`

	// alertsSQL lists alerts newest first, with the island's identity and
	// name joined in so that a caller needs one query, not two.
	alertsSQL = `SELECT a.id, i.session_guid, i.island_id, i.name,
			a.product_guid, a.rule, a.severity, a.raised_at, a.cleared_at, a.detail
		FROM alert a JOIN island i ON i.id = a.island`

	// alertsActiveWhere restricts that list to the open alerts.
	alertsActiveWhere = `a.cleared_at IS NULL`

	// alertsSeverityNot leaves out one severity.
	alertsSeverityNot = `a.severity <> ?`

	// alertsOrder is newest first. id breaks the tie so that two alerts
	// raised in the same millisecond still have a stable order.
	alertsOrder = ` ORDER BY a.raised_at DESC, a.id DESC`
)

// AlertRow is one stored alert: the surrogate id, the island it belongs to,
// and the rule's own fields. ClearedAt is the zero time while the alert is
// open.
type AlertRow struct {
	ID          int64
	Island      model.IslandKey
	IslandName  string
	ProductGUID int32
	Rule        string
	Severity    string
	RaisedAt    time.Time
	ClearedAt   time.Time
	Detail      string
}

// Active reports whether the stored alert is still open.
func (r AlertRow) Active() bool { return r.ClearedAt.IsZero() }

// RaiseAlert records a raised alert and returns its row id.
//
// The island is resolved by its key, and inserted from the alert when it is
// not in the database yet. That case is not expected - the snapshot that
// caused the alert is written first, because both go through the same queue -
// but an alert that could not be stored because its island happened to be
// missing would be a silently lost warning.
func (s *Store) RaiseAlert(ctx context.Context, a alerts.Alert) (int64, error) {
	var id int64
	err := s.write(ctx, func(tx *sql.Tx) error {
		islandID, err := islandRowID(ctx, tx, a)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, insertAlertSQL,
			islandID, a.ProductGUID, a.Rule, a.Severity, millis(a.RaisedAt), a.Detail)
		if err != nil {
			return fmt.Errorf("store: raise alert (island %d, product %d, rule %s): %w",
				islandID, a.ProductGUID, a.Rule, err)
		}
		id, err = res.LastInsertId()
		if err != nil {
			return fmt.Errorf("store: raise alert: %w", err)
		}
		return nil
	})
	return id, err
}

// islandRowID resolves the alert's island to its surrogate id, creating the
// row when it is missing.
func islandRowID(ctx context.Context, tx *sql.Tx, a alerts.Alert) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, islandIDSQL, a.Island.SessionGUID, a.Island.IslandID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("store: look up island (%d,%d): %w",
			a.Island.SessionGUID, a.Island.IslandID, err)
	}

	ts := millis(a.RaisedAt)
	err = tx.QueryRowContext(ctx, upsertIslandSQL,
		a.Island.SessionGUID, a.Island.IslandID, a.IslandName, ts, ts).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: insert island (%d,%d): %w",
			a.Island.SessionGUID, a.Island.IslandID, err)
	}
	return id, nil
}

// ClearAlert closes the open alert for one island, product and rule.
//
// Clearing something that is not open - because it was never raised, or
// because Open already closed it - is not an error: the engine's state and
// the database's can disagree after a restart, and the database is the one
// that has to tolerate it.
func (s *Store) ClearAlert(ctx context.Context, key model.IslandKey, productGUID int32, rule string, at time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		var islandID int64
		err := tx.QueryRowContext(ctx, islandIDSQL, key.SessionGUID, key.IslandID).Scan(&islandID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("store: clear alert: look up island (%d,%d): %w",
				key.SessionGUID, key.IslandID, err)
		}
		if _, err := tx.ExecContext(ctx, clearAlertSQL, millis(at), islandID, productGUID, rule); err != nil {
			return fmt.Errorf("store: clear alert (island %d, product %d, rule %s): %w",
				islandID, productGUID, rule, err)
		}
		return nil
	})
}

// CloseOpenAlerts closes every alert that has no end time. Open calls it: the
// rule engine starts empty after a restart, so an alert left open by the
// previous run has nobody left to clear it.
func (s *Store) CloseOpenAlerts(ctx context.Context, at time.Time) error {
	return s.write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, closeOpenAlertsSQL, millis(at)); err != nil {
			return fmt.Errorf("store: close open alerts: %w", err)
		}
		return nil
	})
}

// Alerts returns stored alerts, newest first. activeOnly restricts the list
// to alerts that are still open; a limit of zero or less returns all of them.
func (s *Store) Alerts(ctx context.Context, activeOnly bool, limit int) ([]AlertRow, error) {
	return s.alerts(ctx, activeOnly, "", limit)
}

// AlertsWithout is Alerts over open and closed alerts, leaving out every
// alert of the given severity. The warning history uses it to keep the info
// alerts out: an island without local production of a good records one for
// every such good, and they would push the warnings out of any limit.
func (s *Store) AlertsWithout(ctx context.Context, severity string, limit int) ([]AlertRow, error) {
	return s.alerts(ctx, false, severity, limit)
}

// alerts builds and runs the list query; an empty skipSeverity leaves
// nothing out.
func (s *Store) alerts(ctx context.Context, activeOnly bool, skipSeverity string, limit int) ([]AlertRow, error) {
	query := alertsSQL
	var where []string
	var args []any
	if activeOnly {
		where = append(where, alertsActiveWhere)
	}
	if skipSeverity != "" {
		where = append(where, alertsSeverityNot)
		args = append(args, skipSeverity)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += alertsOrder
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list alerts: %w", err)
	}
	defer rows.Close()

	var out []AlertRow
	for rows.Next() {
		var r AlertRow
		var raised int64
		var cleared sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Island.SessionGUID, &r.Island.IslandID, &r.IslandName,
			&r.ProductGUID, &r.Rule, &r.Severity, &raised, &cleared, &r.Detail); err != nil {
			return nil, fmt.Errorf("store: list alerts: %w", err)
		}
		r.RaisedAt = fromMillis(raised)
		if cleared.Valid {
			r.ClearedAt = fromMillis(cleared.Int64)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list alerts: %w", err)
	}
	return out, nil
}
