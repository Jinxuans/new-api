package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
)

func TestPaymentReturnURLUsesSuppliedDefaultDashboardPath(t *testing.T) {
	previousAddress := system_setting.ServerAddress
	previousTheme := common.GetTheme()
	system_setting.ServerAddress = "https://dashboard.example.com/"
	common.SetTheme("default")
	t.Cleanup(func() {
		system_setting.ServerAddress = previousAddress
		common.SetTheme(previousTheme)
	})

	assert.Equal(t, "https://dashboard.example.com/wallet", PaymentReturnURL("/wallet"))
}
