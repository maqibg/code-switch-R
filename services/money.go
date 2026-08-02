package services

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// Money 是服务内部唯一的金额类型。JSON/Wails/SQLite 边界使用字符串，
// 这样既保留 Decimal 的精度，又不会把 big.Int 结构暴露给前端绑定生成器。
type Money = decimal.Decimal

func parseMoney(value string) (Money, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return decimal.Zero, nil
	}
	amount, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero, fmt.Errorf("金额不是有效数字: %q: %w", value, err)
	}
	if amount.IsNegative() {
		return decimal.Zero, fmt.Errorf("金额不能为负数: %q", value)
	}
	return amount, nil
}

func parseSignedMoney(value string) (Money, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return decimal.Zero, nil
	}
	amount, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero, err
	}
	return amount, nil
}

func moneyString(value Money) string {
	if value.IsZero() {
		return "0"
	}
	return value.String()
}

const moneyDisplayScale int32 = 6

// moneyLogString 是日志和统计的展示格式：只在返回给前端前四舍五入，
// 数据库和内部计算仍使用 moneyString 的完整精度。
func moneyLogString(value Money) string {
	return moneyString(value.Round(moneyDisplayScale))
}

func formatStoredMoney(value string) string {
	amount, err := parseMoney(value)
	if err != nil {
		return "0"
	}
	return moneyLogString(amount)
}

func moneyStrings(values ...Money) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = moneyString(value)
	}
	return result
}

func sumMoneyList(value string) Money {
	total := decimal.Zero
	for _, item := range strings.Split(value, "|") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if amount, err := parseMoney(item); err == nil {
			total = total.Add(amount)
		}
	}
	return total
}
