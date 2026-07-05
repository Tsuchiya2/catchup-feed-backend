package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"catchup-feed/internal/domain/entity"
	"catchup-feed/internal/repository"
)

const subscriberColumns = "id, name, note, email, created_at, deactivated_at"

// SubscriberRepo persists friends receiving the public feed (§4 / C-8).
type SubscriberRepo struct{ db *sql.DB }

func NewSubscriberRepo(db *sql.DB) repository.SubscriberRepository {
	return &SubscriberRepo{db: db}
}

func scanSubscriber(s scanner) (*entity.Subscriber, error) {
	var subscriber entity.Subscriber
	if err := s.Scan(
		&subscriber.ID, &subscriber.Name, &subscriber.Note, &subscriber.Email,
		&subscriber.CreatedAt, &subscriber.DeactivatedAt,
	); err != nil {
		return nil, err
	}
	return &subscriber, nil
}

// Create inserts the subscriber and sets subscriber.ID / CreatedAt.
func (repo *SubscriberRepo) Create(ctx context.Context, subscriber *entity.Subscriber) error {
	const query = `
INSERT INTO subscribers (name, note, email)
VALUES ($1, $2, $3)
RETURNING id, created_at`
	err := repo.db.QueryRowContext(ctx, query,
		subscriber.Name, subscriber.Note, subscriber.Email,
	).Scan(&subscriber.ID, &subscriber.CreatedAt)
	if err != nil {
		return fmt.Errorf("Create: %w", err)
	}
	return nil
}

// Get returns the subscriber, or nil when not found.
func (repo *SubscriberRepo) Get(ctx context.Context, id int64) (*entity.Subscriber, error) {
	query := `
SELECT ` + subscriberColumns + `
FROM subscribers
WHERE id = $1
LIMIT 1`
	subscriber, err := scanSubscriber(repo.db.QueryRowContext(ctx, query, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("Get: %w", err)
	}
	return subscriber, nil
}

// List returns all subscribers (active and deactivated), oldest first.
func (repo *SubscriberRepo) List(ctx context.Context) ([]*entity.Subscriber, error) {
	query := `
SELECT ` + subscriberColumns + `
FROM subscribers
ORDER BY id ASC`
	rows, err := repo.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("List: %w", err)
	}
	defer func() { _ = rows.Close() }()

	subscribers := make([]*entity.Subscriber, 0, 10)
	for rows.Next() {
		subscriber, err := scanSubscriber(rows)
		if err != nil {
			return nil, fmt.Errorf("List: %w", err)
		}
		subscribers = append(subscribers, subscriber)
	}
	return subscribers, rows.Err()
}

// Update rewrites name / note / email.
func (repo *SubscriberRepo) Update(ctx context.Context, subscriber *entity.Subscriber) error {
	const query = `
UPDATE subscribers SET
       name  = $1,
       note  = $2,
       email = $3
WHERE id = $4`
	res, err := repo.db.ExecContext(ctx, query,
		subscriber.Name, subscriber.Note, subscriber.Email, subscriber.ID,
	)
	if err != nil {
		return fmt.Errorf("Update: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("Update: no rows affected")
	}
	return nil
}

// Deactivate marks the subscriber inactive as of t (idempotent: an already
// deactivated subscriber keeps its original timestamp).
func (repo *SubscriberRepo) Deactivate(ctx context.Context, id int64, t time.Time) error {
	const query = `
UPDATE subscribers SET deactivated_at = $1
WHERE id = $2 AND deactivated_at IS NULL`
	if _, err := repo.db.ExecContext(ctx, query, t, id); err != nil {
		return fmt.Errorf("Deactivate: %w", err)
	}
	return nil
}
