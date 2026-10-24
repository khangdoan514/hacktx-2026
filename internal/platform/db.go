package platform

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type txKey struct{}

type DB struct {
	Pool *pgxpool.Pool
}

func Open(ctx context.Context, databaseURL string) (*DB, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	return &DB{Pool: pool}, nil
}

func (db *DB) Close() {
	db.Pool.Close()
}

func WithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey{}, tx)
}

func Tx(ctx context.Context) pgx.Tx {
	tx, _ := ctx.Value(txKey{}).(pgx.Tx)
	return tx
}

func (db *DB) WithUser(ctx context.Context, userID, email string, fn func(context.Context, pgx.Tx) error) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT set_config('app.user_id', $1, true)", userID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.user_email', $1, true)", email); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE app_runtime"); err != nil {
		return err
	}
	if err := fn(WithTx(ctx, tx), tx); err != nil {
		if mapped := As(err); mapped != nil {
			return mapped
		}
		return err
	}
	return tx.Commit(ctx)
}

func (db *DB) Admin(ctx context.Context, fn func(context.Context, pgx.Tx) error) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := fn(WithTx(ctx, tx), tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func Missing(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}
