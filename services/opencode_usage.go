package services

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"codeswitch/internal/infra"
	modelpricing "codeswitch/resources/model-pricing"
	"github.com/daodao97/xgo/xdb"
)

type openCodeUsageStateFile struct {
	DatabasePath       string           `json:"databasePath"`
	DatabaseModifiedAt int64            `json:"databaseModifiedAt"`
	Sessions           map[string]int64 `json:"sessions"`
}

type openCodeUsageMessage struct {
	MessageID         string
	InputTokens       int
	OutputTokens      int
	ReasoningTokens   int
	CacheCreateTokens int
	CacheReadTokens   int
	KnownMask         int
	Cost              float64
	Model             string
	CreatedAt         time.Time
	UsageJSON         string
}

func (s *OpenCodeService) usageLoggingEnabledLocked() bool {
	if s.appSettings == nil {
		return false
	}
	settings, err := s.appSettings.GetAppSettings()
	if err != nil {
		s.usageState.LastError = err.Error()
		return false
	}
	s.usageState.Enabled = settings.OpenCodeUsageLoggingEnabled
	return settings.OpenCodeUsageLoggingEnabled
}

func (s *OpenCodeService) currentUsageLoggingStateLocked() OpenCodeUsageLoggingState {
	_ = s.usageLoggingEnabledLocked()
	return s.usageState
}

func (s *OpenCodeService) syncUsageLocked() (OpenCodeUsageSyncResult, error) {
	result := OpenCodeUsageSyncResult{Enabled: true, Errors: []string{}}
	dbPath, err := openCodeUsageDatabasePath()
	if err != nil {
		return result, err
	}
	result.DatabasePath = dbPath
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		s.usageState.LastError = ""
		s.usageState.LastSyncAt = time.Now().Format(time.RFC3339)
		return result, nil
	} else if err != nil {
		return result, fmt.Errorf("读取 OpenCode 使用记录数据库失败: %w", err)
	}
	modifiedAt, err := openCodeUsageDatabaseModifiedAt(dbPath)
	if err != nil {
		return result, err
	}
	statePath, err := openCodeUsageStatePath()
	if err != nil {
		return result, err
	}
	state, err := loadOpenCodeUsageState(statePath)
	if err != nil {
		return result, err
	}
	if state.DatabasePath == dbPath && state.DatabaseModifiedAt >= modifiedAt {
		s.usageState.LastError = ""
		s.usageState.LastSyncAt = time.Now().Format(time.RFC3339)
		return result, nil
	}

	conn, err := sql.Open("sqlite", openCodeReadOnlyDSN(dbPath))
	if err != nil {
		return result, fmt.Errorf("打开 OpenCode 使用记录数据库失败: %w", err)
	}
	defer conn.Close()
	if err := conn.Ping(); err != nil {
		return result, fmt.Errorf("连接 OpenCode 使用记录数据库失败: %w", err)
	}
	sessions, err := queryOpenCodeUsageSessions(conn)
	if err != nil {
		return result, err
	}
	result.Sessions = len(sessions)
	if state.Sessions == nil {
		state.Sessions = map[string]int64{}
	}
	stateChanged := false
	for _, session := range sessions {
		if state.Sessions[session.ID] >= session.Watermark {
			continue
		}
		messages, incomplete, queryErr := queryOpenCodeUsageMessages(conn, session.ID)
		if queryErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("会话 %s: %v", session.ID, queryErr))
			continue
		}
		sessionHadErrors := false
		for _, message := range messages {
			inserted, insertErr := s.insertOpenCodeUsageMessage(message, session.ID)
			if insertErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("消息 %s: %v", session.ID, insertErr))
				sessionHadErrors = true
				continue
			}
			if inserted {
				result.Imported++
			} else {
				result.Skipped++
			}
		}
		if !incomplete && !sessionHadErrors {
			state.Sessions[session.ID] = session.Watermark
			stateChanged = true
		}
	}
	if len(result.Errors) == 0 {
		state.DatabasePath = dbPath
		state.DatabaseModifiedAt = modifiedAt
		stateChanged = true
	}
	if stateChanged {
		if err := saveOpenCodeUsageState(statePath, state); err != nil {
			return result, fmt.Errorf("保存 OpenCode 使用记录同步状态失败: %w", err)
		}
	}
	s.usageState.LastSyncAt = time.Now().Format(time.RFC3339)
	s.usageState.LastImported = result.Imported
	s.usageState.LastError = strings.Join(result.Errors, "\n")
	if len(result.Errors) > 0 {
		return result, fmt.Errorf("OpenCode 使用记录同步部分失败: %s", strings.Join(result.Errors, "; "))
	}
	return result, nil
}

type openCodeUsageSession struct {
	ID        string
	Watermark int64
}

func queryOpenCodeUsageSessions(conn *sql.DB) ([]openCodeUsageSession, error) {
	rows, err := conn.Query(`SELECT s.id, MAX(s.time_updated, COALESCE(MAX(m.time_updated), s.time_updated)) AS sync_watermark FROM session s LEFT JOIN message m ON m.session_id = s.id GROUP BY s.id ORDER BY sync_watermark`)
	if err != nil {
		return nil, fmt.Errorf("查询 OpenCode 会话失败: %w", err)
	}
	defer rows.Close()
	result := make([]openCodeUsageSession, 0)
	for rows.Next() {
		var item openCodeUsageSession
		if err := rows.Scan(&item.ID, &item.Watermark); err != nil {
			return nil, fmt.Errorf("读取 OpenCode 会话失败: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func queryOpenCodeUsageMessages(conn *sql.DB, sessionID string) ([]openCodeUsageMessage, bool, error) {
	rows, err := conn.Query(`SELECT id, data FROM message WHERE session_id = ? ORDER BY time_created`, sessionID)
	if err != nil {
		return nil, false, fmt.Errorf("查询 assistant 消息失败: %w", err)
	}
	defer rows.Close()
	result := make([]openCodeUsageMessage, 0)
	incomplete := false
	for rows.Next() {
		var messageID, raw string
		if err := rows.Scan(&messageID, &raw); err != nil {
			return nil, false, fmt.Errorf("读取 assistant 消息失败: %w", err)
		}
		var value map[string]any
		if err := json.Unmarshal([]byte(raw), &value); err != nil || value["role"] != "assistant" || value["tokens"] == nil {
			continue
		}
		timeValue, _ := value["time"].(map[string]any)
		if _, completed := timeValue["completed"]; !completed {
			incomplete = true
			continue
		}
		message, ok := parseOpenCodeUsageMessage(messageID, value)
		if ok {
			result = append(result, message)
		}
	}
	return result, incomplete, rows.Err()
}

func parseOpenCodeUsageMessage(messageID string, value map[string]any) (openCodeUsageMessage, bool) {
	tokens, ok := value["tokens"].(map[string]any)
	if !ok {
		return openCodeUsageMessage{}, false
	}
	readInt := func(container map[string]any, key string) (int, bool) {
		raw, exists := container[key]
		if !exists {
			return 0, false
		}
		switch number := raw.(type) {
		case float64:
			return int(number), true
		case json.Number:
			value, err := number.Int64()
			return int(value), err == nil
		case int:
			return number, true
		case int64:
			return int(number), true
		case uint:
			return int(number), true
		case uint64:
			return int(number), true
		default:
			return 0, false
		}
	}
	input, inputKnown := readInt(tokens, "input")
	output, outputKnown := readInt(tokens, "output")
	reasoning, reasoningKnown := readInt(tokens, "reasoning")
	cacheRead, cacheReadKnown := 0, false
	cacheWrite, cacheWriteKnown := 0, false
	if cache, ok := tokens["cache"].(map[string]any); ok {
		cacheRead, cacheReadKnown = readInt(cache, "read")
		cacheWrite, cacheWriteKnown = readInt(cache, "write")
	}
	if input+output+reasoning+cacheRead+cacheWrite == 0 {
		return openCodeUsageMessage{}, false
	}
	knownMask := 0
	if inputKnown {
		knownMask |= UsageFieldInput
	}
	if outputKnown {
		knownMask |= UsageFieldOutput
	}
	if cacheWriteKnown {
		knownMask |= UsageFieldCacheCreate
	}
	if cacheReadKnown {
		knownMask |= UsageFieldCacheRead
	}
	if reasoningKnown {
		knownMask |= UsageFieldReasoning
	}
	model, _ := value["modelID"].(string)
	if strings.TrimSpace(model) == "" {
		model = "unknown"
	}
	cost, _ := value["cost"].(float64)
	createdAt := time.Unix(0, 0)
	if created, ok := timeValueInt(value, "created"); ok {
		createdAt = time.UnixMilli(created)
	}
	tokensJSON, _ := json.Marshal(tokens)
	return openCodeUsageMessage{
		MessageID:   messageID,
		InputTokens: input, OutputTokens: output, ReasoningTokens: reasoning,
		CacheCreateTokens: cacheWrite, CacheReadTokens: cacheRead, KnownMask: knownMask,
		Cost: cost, Model: model, CreatedAt: createdAt, UsageJSON: string(tokensJSON),
	}, true
}

func timeValueInt(value map[string]any, key string) (int64, bool) {
	timeValue, ok := value["time"].(map[string]any)
	if !ok {
		return 0, false
	}
	raw, ok := timeValue[key].(float64)
	return int64(raw), ok
}

func (s *OpenCodeService) insertOpenCodeUsageMessage(message openCodeUsageMessage, sessionID string) (bool, error) {
	db, err := xdb.DB("default")
	if err != nil {
		return false, fmt.Errorf("获取日志数据库失败: %w", err)
	}
	createdAt := formatCreatedAtBoundary(message.CreatedAt)
	usage := modelpricing.UsageSnapshot{
		InputTokens: message.InputTokens, OutputTokens: message.OutputTokens,
		ReasoningTokens: message.ReasoningTokens, CacheCreateTokens: message.CacheCreateTokens,
		CacheReadTokens: message.CacheReadTokens,
	}
	inputCost, outputCost, reasoningCost, cacheCreateCost, cacheReadCost, totalCost := "0", "0", "0", "0", "0", "0"
	hasPricing := false
	pricingSource := ""
	if message.Cost > 0 {
		totalCost = fmt.Sprintf("%.12f", message.Cost)
		hasPricing = true
		pricingSource = "opencode"
	} else if s.pricing != nil {
		pricing := s.pricing.newRequestSnapshot("opencode", "", message.Model).Calculate(message.Model, usage)
		cost := pricing.Cost
		inputCost, outputCost = moneyString(cost.InputCost), moneyString(cost.OutputCost)
		reasoningCost, cacheCreateCost = moneyString(cost.ReasoningCost), moneyString(cost.CacheCreateCost)
		cacheReadCost, totalCost = moneyString(cost.CacheReadCost), moneyString(cost.TotalCost)
		hasPricing, pricingSource = cost.HasPricing, pricing.Source
	}
	billingStatus := BillingStatusUnpriced
	if hasPricing {
		billingStatus = BillingStatusBillable
	}
	if totalCost == "0" && hasPricing {
		billingStatus = BillingStatusNoCharge
	}
	logEntry := RequestLog{
		RequestID: openCodeUsageRequestID(sessionID, message.MessageID),
		Platform:  "opencode", RequestedModel: message.Model, Model: message.Model,
		Provider: "OpenCode 会话", HttpCode: 200, AttemptCount: 1,
		InputTokens: message.InputTokens, OutputTokens: message.OutputTokens,
		ReasoningTokens: message.ReasoningTokens, CacheCreateTokens: message.CacheCreateTokens,
		CacheReadTokens: message.CacheReadTokens, UsageStatus: UsageStatusComplete,
		UsageKnownMask: message.KnownMask, UsageJSON: message.UsageJSON, IsStream: true,
		CreatedAt: createdAt, InputCost: inputCost, OutputCost: outputCost,
		ReasoningCost: reasoningCost, CacheCreateCost: cacheCreateCost, CacheReadCost: cacheReadCost,
		TotalCost: totalCost, HasPricing: hasPricing, PricingSource: pricingSource,
		BillingStatus: billingStatus,
	}
	statement := RequestLogInsertStatement(logEntry)
	result, err := db.Exec(statement.Query, statement.Args...)
	if err != nil {
		return false, fmt.Errorf("写入 OpenCode 使用记录失败: %w", err)
	}
	rows, err := result.RowsAffected()
	return rows > 0, err
}

func openCodeUsageRequestID(sessionID, messageID string) string {
	return "opencode_session:" + sessionID + ":" + messageID
}

func openCodeUsageDatabasePath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("OPENCODE_DB")); value != "" {
		path, err := filepath.Abs(value)
		if err != nil {
			return "", err
		}
		return filepath.Clean(path), nil
	}
	base, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("获取用户目录失败: %w", err)
	}
	return filepath.Join(base, ".local", "share", "opencode", "opencode.db"), nil
}

func openCodeUsageDatabaseModifiedAt(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	modified := info.ModTime().UnixNano()
	if wal, err := os.Stat(path + "-wal"); err == nil && wal.ModTime().UnixNano() > modified {
		modified = wal.ModTime().UnixNano()
	}
	return modified, nil
}

func openCodeUsageStatePath() (string, error) {
	dir, err := infra.GetAppConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "opencode-usage-state.json"), nil
}

func loadOpenCodeUsageState(path string) (openCodeUsageStateFile, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return openCodeUsageStateFile{Sessions: map[string]int64{}}, nil
	}
	if err != nil {
		return openCodeUsageStateFile{}, err
	}
	var state openCodeUsageStateFile
	if err := json.Unmarshal(data, &state); err != nil {
		return openCodeUsageStateFile{}, fmt.Errorf("解析 OpenCode 使用记录同步状态失败: %w", err)
	}
	if state.Sessions == nil {
		state.Sessions = map[string]int64{}
	}
	return state, nil
}

func saveOpenCodeUsageState(path string, state openCodeUsageStateFile) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWriteFile(path, append(data, '\n'), 0o600)
}

func openCodeReadOnlyDSN(path string) string {
	return "file:" + filepath.ToSlash(path) + "?mode=ro"
}
