package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"reflect"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type healthGroupPayloadMatcher struct {
	want []int64
}

func (m healthGroupPayloadMatcher) Match(value driver.Value) bool {
	raw, ok := value.([]byte)
	if !ok {
		return false
	}
	var payload struct {
		GroupIDs []int64 `json:"group_ids"`
	}
	return json.Unmarshal(raw, &payload) == nil && reflect.DeepEqual(payload.GroupIDs, m.want)
}

func TestAccountHealthRepositoryAssignMissingToGroup(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT EXISTS.*FROM groups`).
		WithArgs(int64(7), service.StatusActive, service.PlatformOpenAI).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery(`(?s)INSERT INTO account_groups.*ON CONFLICT.*RETURNING account_id`).
		WithArgs(int64(7), service.PlatformOpenAI).
		WillReturnRows(sqlmock.NewRows([]string{"account_id"}).AddRow(int64(11)).AddRow(int64(29)))
	for _, accountID := range []int64{11, 29} {
		mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)")).
			WithArgs(service.SchedulerOutboxEventAccountGroupsChanged, accountID, nil, healthGroupPayloadMatcher{want: []int64{7}}).
			WillReturnResult(sqlmock.NewResult(1, 1))
	}
	mock.ExpectCommit()

	repo := &accountHealthRepository{db: db}
	assigned, err := repo.AssignMissingToGroup(context.Background(), 7)

	require.NoError(t, err)
	require.EqualValues(t, 2, assigned)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAccountHealthRepositoryAssignMissingRejectsInactiveTarget(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT EXISTS.*FROM groups`).
		WithArgs(int64(8), service.StatusActive, service.PlatformOpenAI).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))
	mock.ExpectRollback()

	repo := &accountHealthRepository{db: db}
	assigned, err := repo.AssignMissingToGroup(context.Background(), 8)

	require.ErrorContains(t, err, "active OpenAI group")
	require.Zero(t, assigned)
	require.NoError(t, mock.ExpectationsWereMet())
}
