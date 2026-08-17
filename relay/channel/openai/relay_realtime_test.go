package openai

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type realtimeBillingRecorder struct {
	preConsumedQuota int
	reserveTargets   []int
	reserveErr       error
}

func (*realtimeBillingRecorder) Settle(int) error           { return nil }
func (*realtimeBillingRecorder) Refund(*gin.Context)        {}
func (*realtimeBillingRecorder) NeedsRefund() bool          { return false }
func (r *realtimeBillingRecorder) GetPreConsumedQuota() int { return r.preConsumedQuota }

func (r *realtimeBillingRecorder) Reserve(targetQuota int) error {
	r.reserveTargets = append(r.reserveTargets, targetQuota)
	if r.reserveErr != nil {
		return r.reserveErr
	}
	if targetQuota > r.preConsumedQuota {
		r.preConsumedQuota = targetQuota
	}
	return nil
}

func (*realtimeBillingRecorder) ConfirmDispatch() error { return nil }

func (r *realtimeBillingRecorder) ReserveUsage(targetQuota int) error {
	return r.Reserve(targetQuota)
}

func TestRealtimeSegmentsReserveCumulativeUsageTarget(t *testing.T) {
	originalModelRatios := ratio_setting.ModelRatio2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"realtime-cumulative-model":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"realtime-cumulative-group":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})

	billing := &realtimeBillingRecorder{}
	info := &relaycommon.RelayInfo{
		OriginModelName: "realtime-cumulative-model",
		UsingGroup:      "realtime-cumulative-group",
		UserGroup:       "realtime-cumulative-user-group",
		BillingSource:   service.BillingSourceSubscription,
		Billing:         billing,
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	totalUsage := &dto.RealtimeUsage{}

	require.NoError(t, preConsumeUsage(ctx, info, &dto.RealtimeUsage{
		TotalTokens: 10,
		InputTokens: 10,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens: 10,
		},
	}, totalUsage))
	require.NoError(t, preConsumeUsage(ctx, info, &dto.RealtimeUsage{
		TotalTokens: 20,
		InputTokens: 20,
		InputTokenDetails: dto.InputTokenDetails{
			TextTokens: 20,
		},
	}, totalUsage))

	assert.Equal(t, []int{10, 30}, billing.reserveTargets)
	assert.Equal(t, 30, totalUsage.TotalTokens)
	assert.Equal(t, 30, totalUsage.InputTokens)
	assert.Equal(t, 30, totalUsage.InputTokenDetails.TextTokens)
}

func TestRealtimeReserveFailureDoesNotCountAuthoritativeUsageTwice(t *testing.T) {
	originalModelRatios := ratio_setting.ModelRatio2JSONString()
	originalGroupRatios := ratio_setting.GroupRatio2JSONString()
	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(`{"realtime-reserve-failure-model":1}`))
	require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(`{"realtime-reserve-failure-group":1}`))
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(originalModelRatios))
		require.NoError(t, ratio_setting.UpdateGroupRatioByJSONString(originalGroupRatios))
	})

	clientHandler, clientPeer := newRealtimeWebSocketPair(t)
	targetHandler, targetPeer := newRealtimeWebSocketPair(t)
	billing := &realtimeBillingRecorder{
		preConsumedQuota: 5,
		reserveErr:       errors.New("reservation denied"),
	}
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	info := &relaycommon.RelayInfo{
		OriginModelName: "realtime-reserve-failure-model",
		UsingGroup:      "realtime-reserve-failure-group",
		UserGroup:       "realtime-reserve-failure-user-group",
		BillingSource:   service.BillingSourceSubscription,
		Billing:         billing,
		ClientWs:        clientHandler,
		TargetWs:        targetHandler,
	}

	type handlerResult struct {
		usage *dto.RealtimeUsage
	}
	resultCh := make(chan handlerResult, 1)
	go func() {
		_, usage := OpenaiRealtimeHandler(ctx, info)
		resultCh <- handlerResult{usage: usage}
	}()
	require.NoError(t, targetPeer.WriteJSON(dto.RealtimeEvent{
		Type: dto.RealtimeEventTypeResponseDone,
		Response: &dto.RealtimeResponse{Usage: &dto.RealtimeUsage{
			TotalTokens: 30,
			InputTokens: 30,
			InputTokenDetails: dto.InputTokenDetails{
				TextTokens: 30,
			},
		}},
	}))

	select {
	case result := <-resultCh:
		require.NotNil(t, result.usage)
		assert.Equal(t, 30, result.usage.TotalTokens)
		assert.Equal(t, 30, result.usage.InputTokens)
		assert.Equal(t, 30, result.usage.InputTokenDetails.TextTokens)
		assert.Equal(t, []int{30}, billing.reserveTargets)
	case <-time.After(3 * time.Second):
		t.Fatal("realtime handler did not stop after reservation failure")
	}

	require.NoError(t, clientPeer.Close())
	require.NoError(t, targetPeer.Close())
}

func newRealtimeWebSocketPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	type upgradeResult struct {
		connection *websocket.Conn
		err        error
	}
	resultCh := make(chan upgradeResult, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := (&websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}).Upgrade(w, r, nil)
		resultCh <- upgradeResult{connection: connection, err: err}
	}))
	t.Cleanup(server.Close)

	peer, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	result := <-resultCh
	require.NoError(t, result.err)
	require.NotNil(t, result.connection)
	t.Cleanup(func() {
		_ = result.connection.Close()
		_ = peer.Close()
	})
	return result.connection, peer
}
