package services

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestMoneyLogStringRoundsToSixPlacesAndTrimsZeros(t *testing.T) {
	cases := map[string]string{
		"1.2345674": "1.234567",
		"1.2345675": "1.234568",
		"1.230000":  "1.23",
		"0":         "0",
	}
	for raw, want := range cases {
		amount, err := decimal.NewFromString(raw)
		if err != nil {
			t.Fatalf("解析金额 %q 失败: %v", raw, err)
		}
		if got := moneyLogString(amount); got != want {
			t.Errorf("moneyLogString(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestFormatStoredMoneyInvalidValueReturnsZero(t *testing.T) {
	if got := formatStoredMoney("not-a-number"); got != "0" {
		t.Fatalf("非法存储金额应显示为 0，实际 %q", got)
	}
}
