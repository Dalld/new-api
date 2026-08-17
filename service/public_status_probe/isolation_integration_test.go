package public_status_probe

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	publicstatusprobesetting "github.com/QuantumNous/new-api/setting/public_status_probe_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type isolationProviderBehavior string

const (
	isolationProviderSuccess   isolationProviderBehavior = "success"
	isolationProviderRejection isolationProviderBehavior = "provider_rejection"
	isolationProviderTimeout   isolationProviderBehavior = "timeout"
)

type isolationBusinessSnapshot map[string][]string

func TestPublicStatusProbeIsolationAcrossLoaderAdapterAndPersistence(t *testing.T) {
	tests := []struct {
		name          string
		behavior      isolationProviderBehavior
		wantState     model.PublicStatusProbeState
		wantErrorCode ErrorCode
	}{
		{
			name:      "success",
			behavior:  isolationProviderSuccess,
			wantState: model.PublicStatusProbeStateOperational,
		},
		{
			name:          "provider rejection",
			behavior:      isolationProviderRejection,
			wantState:     model.PublicStatusProbeStateFailed,
			wantErrorCode: ErrorProviderRejected,
		},
		{
			name:          "timeout",
			behavior:      isolationProviderTimeout,
			wantState:     model.PublicStatusProbeStateFailed,
			wantErrorCode: ErrorTimeout,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := setupIsolationDatabase(t)
			server, providerRequests, authorization := newIsolationProvider(t, test.behavior)
			target := seedIsolationBusinessState(t, db, server.URL)

			before := snapshotIsolationBusinessTables(t, db)
			assertIsolationFixtureCoverage(t, db, before)

			setting := publicstatusprobesetting.Setting{
				Enabled:         true,
				Interval:        time.Minute,
				PingTimeout:     250 * time.Millisecond,
				ChatTimeout:     50 * time.Millisecond,
				DegradedLatency: time.Second,
				Concurrency:     1,
				RetentionDays:   7,
				Targets:         []publicstatusprobesetting.Target{target},
			}
			scheduler, err := NewScheduler(
				&fakeSettingProvider{setting: setting},
				NewDBTargetLoader(db),
				modelProbeRepository{},
				server.Client(),
			)
			require.NoError(t, err)
			fixedNow := time.Unix(1_700_000_050, 0).UTC()
			scheduler.now = func() time.Time { return fixedNow }

			require.True(t, scheduler.runSlot(context.Background(), time.Unix(1_700_000_040, 0).UTC()))

			after := snapshotIsolationBusinessTables(t, db)
			assert.Equal(t, before, after, "a public probe must not mutate non-probe business tables")
			assert.EqualValues(t, 1, providerRequests.Load(), "the real provider adapter should run exactly once")
			assert.Equal(t, "Bearer provider-key-b", authorization())

			var results []model.PublicStatusProbeResult
			require.NoError(t, db.Find(&results).Error)
			require.Len(t, results, 1)
			assert.Equal(t, target.Key, results[0].TargetKey)
			assert.Equal(t, test.wantState, results[0].State)
			assert.Equal(t, string(test.wantErrorCode), results[0].ErrorCode)
			assert.NotNil(t, results[0].PingLatencyMS)
			if test.wantErrorCode == "" {
				assert.NotNil(t, results[0].ChatLatencyMS)
			} else {
				assert.Nil(t, results[0].ChatLatencyMS)
			}

			var leases []model.PublicStatusProbeLease
			require.NoError(t, db.Find(&leases).Error)
			require.Len(t, leases, 1)
			assert.Equal(t, target.Key, leases[0].TargetKey)
			assert.Empty(t, leases[0].OwnerID)
		})
	}
}

func setupIsolationDatabase(t *testing.T) *gorm.DB {
	t.Helper()
	previousDB := model.DB
	previousLogDB := model.LOG_DB
	previousType := common.MainDatabaseType()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared&_busy_timeout=30000", strings.NewReplacer("/", "_", "\\", "_").Replace(t.Name()))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(8)

	model.DB = db
	model.LOG_DB = db
	common.SetMainDatabaseType(common.DatabaseTypeSQLite)
	require.NoError(t, db.AutoMigrate(
		&model.Channel{},
		&model.User{},
		&model.Token{},
		&model.UserSubscription{},
		&model.SubscriptionPreConsumeRecord{},
		&model.Log{},
		&model.QuotaData{},
		&model.PublicStatusProbeResult{},
		&model.PublicStatusProbeLease{},
	))
	t.Cleanup(func() {
		model.DB = previousDB
		model.LOG_DB = previousLogDB
		common.SetMainDatabaseType(previousType)
		_ = sqlDB.Close()
	})
	return db
}

func newIsolationProvider(t *testing.T, behavior isolationProviderBehavior) (*httptest.Server, *atomic.Int32, func() string) {
	t.Helper()
	var providerRequests atomic.Int32
	var mutex sync.Mutex
	lastAuthorization := ""
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodHead || request.Method == http.MethodGet {
			writer.WriteHeader(http.StatusNoContent)
			return
		}

		providerRequests.Add(1)
		mutex.Lock()
		lastAuthorization = request.Header.Get("Authorization")
		mutex.Unlock()
		switch behavior {
		case isolationProviderSuccess:
			var payload struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			if json.NewDecoder(request.Body).Decode(&payload) != nil || len(payload.Messages) != 1 {
				writer.WriteHeader(http.StatusBadRequest)
				return
			}
			token := strings.TrimPrefix(payload.Messages[0].Content, "Reply with exactly this token and no other text: ")
			writer.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"choices": []any{map[string]any{"message": map[string]any{"content": token}}},
			})
		case isolationProviderRejection:
			writer.WriteHeader(http.StatusUnauthorized)
			_, _ = writer.Write([]byte(`{"error":"rejected"}`))
		case isolationProviderTimeout:
			time.Sleep(150 * time.Millisecond)
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"choices":[{"message":{"content":"too-late"}}]}`))
		default:
			writer.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	return server, &providerRequests, func() string {
		mutex.Lock()
		defer mutex.Unlock()
		return lastAuthorization
	}
}

func seedIsolationBusinessState(t *testing.T, db *gorm.DB, baseURL string) publicstatusprobesetting.Target {
	t.Helper()
	channel := model.Channel{
		Id:           42,
		Type:         constant.ChannelTypeOpenAI,
		Key:          "provider-key-a\nprovider-key-b",
		Status:       common.ChannelStatusEnabled,
		Name:         "isolation-channel",
		TestTime:     1_699_999_900,
		ResponseTime: 987,
		BaseURL:      &baseURL,
		Balance:      12.5,
		Models:       "provider-model,other-model",
		Group:        "default",
		UsedQuota:    4567,
		ChannelInfo: model.ChannelInfo{
			IsMultiKey:             true,
			MultiKeySize:           2,
			MultiKeyStatusList:     map[int]int{0: common.ChannelStatusManuallyDisabled, 1: common.ChannelStatusEnabled},
			MultiKeyDisabledReason: map[int]string{0: "existing provider rejection"},
			MultiKeyDisabledTime:   map[int]int64{0: 1_699_999_800},
			MultiKeyPollingIndex:   1,
		},
	}
	user := model.User{
		Id:              7,
		Username:        "probe-isolation-user",
		Password:        "hashed-password",
		DisplayName:     "Isolation User",
		Role:            common.RoleCommonUser,
		Status:          common.UserStatusEnabled,
		Quota:           100_000,
		UsedQuota:       12_345,
		RequestCount:    91,
		Group:           "default",
		AffQuota:        222,
		AffHistoryQuota: 333,
	}
	token := model.Token{
		Id:             8,
		UserId:         user.Id,
		Key:            "isolation-token-key",
		Status:         common.TokenStatusEnabled,
		Name:           "isolation-token",
		RemainQuota:    88_888,
		UnlimitedQuota: false,
		UsedQuota:      11_112,
		Group:          "default",
	}
	subscription := model.UserSubscription{
		Id:                  9,
		UserId:              user.Id,
		PlanId:              3,
		AmountTotal:         500_000,
		AmountUsed:          123_456,
		StartTime:           1_699_000_000,
		EndTime:             1_800_000_000,
		Status:              "active",
		Source:              "admin",
		LastResetTime:       1_699_500_000,
		NextResetTime:       1_702_000_000,
		AllowWalletOverflow: true,
	}
	preConsume := model.SubscriptionPreConsumeRecord{
		Id:                 12,
		RequestId:          "existing-pre-consume-request",
		UserId:             user.Id,
		UserSubscriptionId: subscription.Id,
		PreConsumed:        654,
		Status:             "consumed",
	}
	log := model.Log{
		Id:        10,
		UserId:    user.Id,
		CreatedAt: 1_699_999_950,
		Type:      model.LogTypeConsume,
		Content:   "existing consume log",
		Username:  user.Username,
		TokenName: token.Name,
		ModelName: "existing-model",
		Quota:     321,
		ChannelId: channel.Id,
		TokenId:   token.Id,
		Group:     "default",
		RequestId: "existing-request-id",
	}
	quotaData := model.QuotaData{
		Id:        11,
		UserID:    user.Id,
		Username:  user.Username,
		ModelName: "existing-model",
		CreatedAt: 1_699_999_950,
		UseGroup:  "default",
		TokenID:   token.Id,
		ChannelID: channel.Id,
		NodeName:  "existing-node",
		TokenUsed: 17,
		Count:     1,
		Quota:     321,
	}
	for _, row := range []any{&channel, &user, &token, &subscription, &preConsume, &log, &quotaData} {
		require.NoError(t, db.Create(row).Error)
	}

	return publicstatusprobesetting.Target{
		Key:         "isolation-target",
		Group:       "production",
		DisplayName: "Isolation Target",
		Model:       "provider-model",
		Protocol:    publicstatusprobesetting.ProtocolOpenAIChat,
		ChannelID:   channel.Id,
		KeyIndex:    1,
		Enabled:     true,
	}
}

func snapshotIsolationBusinessTables(t *testing.T, db *gorm.DB) isolationBusinessSnapshot {
	t.Helper()
	tables, err := db.Migrator().GetTables()
	require.NoError(t, err)
	snapshot := make(isolationBusinessSnapshot)
	for _, table := range tables {
		if strings.HasPrefix(strings.ToLower(table), "sqlite_") || table == (model.PublicStatusProbeResult{}).TableName() || table == (model.PublicStatusProbeLease{}).TableName() {
			continue
		}
		rows, err := db.Table(table).Rows()
		require.NoError(t, err, "read table %s", table)
		snapshot[table] = canonicalIsolationRows(t, rows)
		require.NoError(t, rows.Close())
	}
	return snapshot
}

func canonicalIsolationRows(t *testing.T, rows *sql.Rows) []string {
	t.Helper()
	columns, err := rows.Columns()
	require.NoError(t, err)
	result := make([]string, 0)
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		require.NoError(t, rows.Scan(destinations...))
		row := make(map[string]string, len(columns))
		for index, column := range columns {
			switch value := values[index].(type) {
			case nil:
				row[column] = "<NULL>"
			case []byte:
				row[column] = "[]byte:" + string(value)
			case time.Time:
				row[column] = "time.Time:" + value.UTC().Format(time.RFC3339Nano)
			default:
				row[column] = fmt.Sprintf("%T:%v", value, value)
			}
		}
		encoded, err := json.Marshal(row)
		require.NoError(t, err)
		result = append(result, string(encoded))
	}
	require.NoError(t, rows.Err())
	sort.Strings(result)
	return result
}

func assertIsolationFixtureCoverage(t *testing.T, db *gorm.DB, snapshot isolationBusinessSnapshot) {
	t.Helper()
	for _, value := range []any{
		&model.Channel{},
		&model.User{},
		&model.Token{},
		&model.UserSubscription{},
		&model.SubscriptionPreConsumeRecord{},
		&model.Log{},
		&model.QuotaData{},
	} {
		statement := &gorm.Statement{DB: db}
		require.NoError(t, statement.Parse(value))
		rows, exists := snapshot[statement.Schema.Table]
		assert.True(t, exists, "sensitive table %s must be covered by the isolation snapshot", statement.Schema.Table)
		assert.NotEmpty(t, rows, "sensitive table %s must contain a sentinel row", statement.Schema.Table)
	}
}
