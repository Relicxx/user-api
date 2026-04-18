// Package db provides database connection and query functions.
package db

import (
	"database/sql"
	"os"

	"user-api/internal/model"

	_ "github.com/jackc/pgx/v5/stdlib"
)

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

func CreateUser(db *sql.DB, user *model.User) error {
	query := `INSERT INTO users
	(name, email)
	VALUES ($1, $2)`
	_, err := db.Exec(query, user.Name, user.Email)

	return err
}

func GetUsers(db *sql.DB) ([]model.User, error) {
	query := `SELECT id, name, email
	FROM users`
	rows, err := db.Query(query)
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

func GetUserByID(db *sql.DB, id int) (*model.User, error) {
	query := `SELECT id, name, email
	FROM users
	WHERE id = $1`

	var user model.User
	err := db.QueryRow(query, id).Scan(&user.ID, &user.Name, &user.Email)
	if err != nil {
		return nil, err
	}

	return &user, nil
}

func UpdateUser(db *sql.DB, user *model.User) error {
	query := `UPDATE users
	SET name = $1, email = $2
	WHERE id = $3`
	_, err := db.Exec(query, user.Name, user.Email, user.ID)

	return err
}

func DeleteUser(db *sql.DB, id int) error {
	query := `DELETE FROM users
	WHERE id = $1`
	_, err := db.Exec(query, id)

	return err
}
