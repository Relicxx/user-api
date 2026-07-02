package db

import (
	"context"
	"database/sql"
	"errors"
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

func (s *UserStorage) CreateUser(ctx context.Context, user *model.User) error {
	query := `INSERT INTO users
	(name, email)
	VALUES ($1, $2)
	RETURNING id`

	err := s.DB.QueryRowContext(ctx, query, user.Name, user.Email).Scan(&user.ID)
	if isUniqueViolation(err) {
		return ErrDuplicateEmail
	}

	return err
}

func (s *UserStorage) GetUsers(ctx context.Context) ([]model.User, error) {
	query := `SELECT id, name, email
	FROM users
	ORDER BY id`

	rows, err := s.DB.QueryContext(ctx, query)
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
