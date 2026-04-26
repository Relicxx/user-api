package db

import (
	"database/sql"
	"os"

	"user-api/internal/model"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type UserStorage struct {
	DB *sql.DB
}

func ConnectDB() (*sql.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	err = db.Ping()
	if err != nil {
		return nil, err
	}

	return db, nil
}

func (s *UserStorage) CreateUser(user *model.User) error {
	query := `INSERT INTO users
	(name, email)
	VALUES ($1, $2)`
	_, err := s.DB.Exec(query, user.Name, user.Email)

	return err
}

func (s *UserStorage) GetUsers() ([]model.User, error) {
	query := `SELECT id, name, email
	FROM users`
	rows, err := s.DB.Query(query)
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

func (s *UserStorage) GetUserByID(id int) (*model.User, error) {
	query := `SELECT id, name, email
	FROM users
	WHERE id = $1`

	var user model.User
	err := s.DB.QueryRow(query, id).Scan(&user.ID, &user.Name, &user.Email)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func (s *UserStorage) UpdateUser(user *model.User) error {
	query := `UPDATE users
	SET name = $1, email = $2
	WHERE id = $3`
	_, err := s.DB.Exec(query, user.Name, user.Email, user.ID)

	return err
}

func (s *UserStorage) DeleteUser(id int) error {
	query := `DELETE FROM users
	WHERE id = $1`
	_, err := s.DB.Exec(query, id)

	return err
}
