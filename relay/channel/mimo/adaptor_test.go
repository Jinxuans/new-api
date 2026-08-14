package mimo

import (
	"net/http"
	"net/http/httptest"
	"testing"

	channelconstant "github.com/QuantumNous/new-api/constant"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

func TestGetRequestURLClaudeUsesMiMOAnthropicPath(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelBaseUrl: "https://api.xiaomimimo.com",
		},
	}

	got, err := adaptor.GetRequestURL(info)
	if err != nil {
		t.Fatalf("GetRequestURL returned error: %v", err)
	}

	want := "https://api.xiaomimimo.com/anthropic/v1/messages"
	if got != want {
		t.Fatalf("GetRequestURL() = %q, want %q", got, want)
	}
}

func TestGetRequestURLOpenAIUsesOpenAICompatiblePath(t *testing.T) {
	t.Parallel()

	adaptor := &Adaptor{}
	info := &relaycommon.RelayInfo{
		RelayFormat:    types.RelayFormatOpenAI,
		RelayMode:      relayconstant.RelayModeChatCompletions,
		RequestURLPath: "/v1/chat/completions",
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelType:    channelconstant.ChannelTypeMiMO,
			ChannelBaseUrl: "https://api.xiaomimimo.com",
		},
	}

	got, err := adaptor.GetRequestURL(info)
	if err != nil {
		t.Fatalf("GetRequestURL returned error: %v", err)
	}

	want := "https://api.xiaomimimo.com/v1/chat/completions"
	if got != want {
		t.Fatalf("GetRequestURL() = %q, want %q", got, want)
	}
}

func TestSetupRequestHeaderUsesProtocolSpecificAuth(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)

	adaptor := &Adaptor{}
	claudeHeaders := http.Header{}
	claudeInfo := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatClaude,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey: "mimo-key",
		},
	}
	if err := adaptor.SetupRequestHeader(c, &claudeHeaders, claudeInfo); err != nil {
		t.Fatalf("SetupRequestHeader Claude returned error: %v", err)
	}
	if got := claudeHeaders.Get("x-api-key"); got != "mimo-key" {
		t.Fatalf("Claude x-api-key = %q, want %q", got, "mimo-key")
	}
	if got := claudeHeaders.Get("Authorization"); got != "" {
		t.Fatalf("Claude Authorization = %q, want empty", got)
	}

	openAIHeaders := http.Header{}
	openAIInfo := &relaycommon.RelayInfo{
		RelayFormat: types.RelayFormatOpenAI,
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiKey: "mimo-key",
		},
	}
	if err := adaptor.SetupRequestHeader(c, &openAIHeaders, openAIInfo); err != nil {
		t.Fatalf("SetupRequestHeader OpenAI returned error: %v", err)
	}
	if got := openAIHeaders.Get("Authorization"); got != "Bearer mimo-key" {
		t.Fatalf("OpenAI Authorization = %q, want %q", got, "Bearer mimo-key")
	}
	if got := openAIHeaders.Get("x-api-key"); got != "" {
		t.Fatalf("OpenAI x-api-key = %q, want empty", got)
	}
}
