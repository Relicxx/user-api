package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"user-api/internal/model"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
)

var ErrDuplicateEmail = errors.New("email already exists")

const uniqueViolationCode = "23505"

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolationCode
}

const (
	maxOpenConns    = 25
	maxIdleConns    = 25
	connMaxLifetime = 5 * time.Minute
	pingTimeout     = 5 * time.Second
)

type UserStorage struct {
	DB *sql.DB
	// EventTopic is the Kafka topic recorded in outbox rows for user events.
	EventTopic string
}

func ConnectDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxIdleConns)
	db.SetConnMaxLifetime(connMaxLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func (s *UserStorage) Ping(ctx context.Context) error {
	return s.DB.PingContext(ctx)
}

// CreateUser inserts the user and its user-created event into the outbox
// in a single transaction, so the event is never lost and never emitted
// for a rolled-back insert (transactional outbox).
func (s *UserStorage) CreateUser(ctx context.Context, user *model.User) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	query := `INSERT INTO users
	(name, email)
	VALUES ($1, $2)
	RETURNING id`

	err = tx.QueryRowContext(ctx, query, user.Name, user.Email).Scan(&user.ID)
	if isUniqueViolation(err) {
		return ErrDuplicateEmail
	}
	if err != nil {
		return err
	}

	payload, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("marshal user event: %w", err)
	}

	// Key is the user ID so all events for one user land in the same
	// partition and keep their order.
	if _, err = tx.ExecContext(ctx,
		`INSERT INTO outbox (topic, key, payload) VALUES ($1, $2, $3)`,
		s.EventTopic, strconv.Itoa(user.ID), payload); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}

	return tx.Commit()
}

func (s *UserStorage) GetUsers(ctx context.Context, limit, offset int) ([]model.User, error) {
	query := `SELECT id, name, email
	FROM users
	ORDER BY id
	LIMIT $1 OFFSET $2`

	rows, err := s.DB.QueryContext(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []model.User

	for rows.Next() {
		var user model.User

		err := rows.Scan(&user.ID, &user.Name, &user.Email)
		if err != nil {
			return nil, err
		}

		users = append(users, user)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return users, nil
}

func (s *UserStorage) GetUserByID(ctx context.Context, id int) (*model.User, error) {
	query := `SELECT id, name, email
	FROM users
	WHERE id = $1`

	var user model.User
	err := s.DB.QueryRowContext(ctx, query, id).Scan(&user.ID, &user.Name, &user.Email)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (s *UserStorage) UpdateUser(ctx context.Context, user *model.User) error {
	query := `UPDATE users
	SET name = $1, email = $2, updated_at = now()
	WHERE id = $3`

	res, err := s.DB.ExecContext(ctx, query, user.Name, user.Email, user.ID)
	if isUniqueViolation(err) {
		return ErrDuplicateEmail
	}
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}

func (s *UserStorage) DeleteUser(ctx context.Context, id int) error {
	query := `DELETE FROM users
	WHERE id = $1`

	res, err := s.DB.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}

	return nil
}
