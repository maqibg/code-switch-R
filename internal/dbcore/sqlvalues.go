package dbcore

// BoolToInt 把布尔转成 SQLite 的 0/1 整数列值
func BoolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// NullableID 把 0 转成 SQL NULL。
//
// 用于"关联不到目标行"语义上不同于 id=0 的外键列：目标行已删除、
// 或来源尚未并入目标表时应写 NULL，写 0 会造出指向不存在行的假外键值。
func NullableID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
