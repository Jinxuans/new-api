package controller

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlaygroundRejectsUserOnRefundHoldBeforeRelay(t *testing.T) {
	db := setupRegisterControllerTestDB(t)
	require.NoError(t, i18n.Init())

	user := &model.User{
		Username:   "playground-refund-held-user",
		AffCode:    "playground-refund-held-user",
		Status:     common.UserStatusEnabled,
		RefundHold: true,
	}
	require.NoError(t, db.Create(user).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/pg/chat/completions", strings.NewReader(`{}`))
	ctx.Request.Header.Set("Accept-Language", i18n.LangEn)
	ctx.Set("id", user.Id)

	Playground(ctx)

	assert.Equal(t, http.StatusForbidden, recorder.Code)
	var response struct {
		Error types.OpenAIError `json:"error"`
	}
	require.NoError(t, common.Unmarshal(recorder.Body.Bytes(), &response))
	assert.Equal(t, string(types.ErrorCodeAccessDenied), response.Error.Code)
	assert.Equal(t, i18n.Translate(i18n.LangEn, i18n.MsgAuthRefundHold), response.Error.Message)
}
