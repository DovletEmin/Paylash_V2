package db

import "paylash/internal/models"

// SavePushSubscription upserts a browser push subscription by its unique
// endpoint — a re-subscribe (new keys, or a different user on the same
// browser) overwrites the previous row rather than duplicating it.
func (d *DB) SavePushSubscription(userID int, endpoint, p256dh, auth string) error {
	_, err := d.Exec(
		`INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (endpoint) DO UPDATE SET user_id = $1, p256dh = $3, auth = $4`,
		userID, endpoint, p256dh, auth,
	)
	return err
}

// DeletePushSubscription removes a subscription by endpoint and owning user —
// used both when the client unsubscribes and when a push service reports the
// endpoint gone (HTTP 404/410). Scoped to userID (not endpoint alone) so an
// authenticated user can never unsubscribe someone else's device by guessing
// or replaying another browser's endpoint URL.
func (d *DB) DeletePushSubscription(userID int, endpoint string) error {
	_, err := d.Exec(`DELETE FROM push_subscriptions WHERE user_id = $1 AND endpoint = $2`, userID, endpoint)
	return err
}

// ListPushSubscriptions returns every push subscription belonging to a user
// (they may have several browsers/devices).
func (d *DB) ListPushSubscriptions(userID int) ([]models.PushSubscription, error) {
	rows, err := d.Query(
		`SELECT id, user_id, endpoint, p256dh, auth, created_at FROM push_subscriptions WHERE user_id = $1`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var subs []models.PushSubscription
	for rows.Next() {
		var s models.PushSubscription
		if err := rows.Scan(&s.ID, &s.UserID, &s.Endpoint, &s.P256dh, &s.Auth, &s.CreatedAt); err != nil {
			return nil, err
		}
		subs = append(subs, s)
	}
	return subs, rows.Err()
}
