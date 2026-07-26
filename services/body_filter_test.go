package services

import (
	"bytes"
	"testing"

	"github.com/tidwall/gjson"
)

func TestFilterPrivateRequestFieldsReturnsOriginalBodyWithoutPrivateKeys(t *testing.T) {
	body := []byte(`{"model":"gpt-test","messages":[{"role":"user","content":"hello"}]}`)
	filtered, err := filterPrivateRequestFields(body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(filtered, body) {
		t.Fatalf("过滤结果发生变化: %s", filtered)
	}
	if len(filtered) > 0 && &filtered[0] != &body[0] {
		t.Fatal("无私有字段时没有复用原始请求体")
	}
}

func TestFilterPrivateRequestFields(t *testing.T) {
	body := []byte(`{"model":"gpt-5","_trace":"secret","messages":[{"role":"user","_private":1}],"tools":[{"function":{"parameters":{"properties":{"_field":{"type":"string"}},"$defs":{"_node":{"type":"object"}}}}}]}`)
	filtered, err := filterPrivateRequestFields(body)
	if err != nil {
		t.Fatal(err)
	}
	if gjson.GetBytes(filtered, "_trace").Exists() || gjson.GetBytes(filtered, "messages.0._private").Exists() {
		t.Fatalf("私有字段未删除: %s", filtered)
	}
	if !gjson.GetBytes(filtered, "tools.0.function.parameters.properties._field").Exists() ||
		!gjson.GetBytes(filtered, "tools.0.function.parameters.$defs._node").Exists() {
		t.Fatalf("JSON Schema 字段名被错误删除: %s", filtered)
	}
}

func TestFilterPrivateRequestFieldsRejectsInvalidJSON(t *testing.T) {
	if _, err := filterPrivateRequestFields([]byte(`{"model":`)); err == nil {
		t.Fatal("无效 JSON 应返回错误")
	}
}
