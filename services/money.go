package services

import (
	"fmt"
	"strconv"
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

func moneyFromLegacyFloat(value float64) Money {
	if value == 0 {
		return decimal.Zero
	}
	// 旧 REAL 值只能恢复数据库中可见的十进制文本，不能伪造原始精度。
	valueText := strconv.FormatFloat(value, 'f', -1, 64)
	amount, err := decimal.NewFromString(valueText)
	if err != nil || amount.IsNegative() {
		return decimal.Zero
	}
	return amount
}

func moneyStrings(values ...Money) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = moneyString(value)
	}
	return result
}

func parseMoneyOrLegacy(value string) Money {
	amount, err := parseMoney(value)
	if err == nil {
		return amount
	}
	return decimal.Zero
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
		} else {
			total = total.Add(moneyFromLegacyText(item))
		}
	}
	return total
}

func moneyFromLegacyText(value string) Money {
	amount, err := decimal.NewFromString(strings.TrimSpace(value))
	if err != nil || amount.IsNegative() {
		return decimal.Zero
	}
	return amount
}

// decimalMoneySQL 返回迁移期间读取金额的 SQL 表达式。
// 新列尚未回填时使用旧 REAL 列，避免后台迁移进行中统计暂时变成 0。
// 两个参数只允许传入源码中的固定列名，不能接收用户输入。
func decimalMoneySQL(exactColumn, legacyColumn string) string {
	return "CASE WHEN " + exactColumn + " IS NULL OR TRIM(" + exactColumn + ") = '' OR (" + exactColumn + " = '0' AND COALESCE(" + legacyColumn + ", 0) <> 0) THEN printf('%.17g', COALESCE(" + legacyColumn + ", 0)) ELSE " + exactColumn + " END"
}
