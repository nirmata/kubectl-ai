package gollm

import (
	"encoding/json"
	"testing"
)

func TestSchemaMarshalUsesRaw(t *testing.T) {
	raw := `{"type":"object","additionalProperties":false,"properties":{"severity":{"type":"string","enum":["low","high"]}}}`
	s := &Schema{Type: TypeObject, Raw: json.RawMessage(raw)}
	got, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != raw {
		t.Fatalf("got %s", got)
	}
	fn := FunctionDefinition{Name: "f", Parameters: s}
	out, _ := json.Marshal(fn)
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	params := m["parameters"].(map[string]any)
	if params["additionalProperties"] != false {
		t.Fatalf("additionalProperties lost: %s", out)
	}
}

func TestSchemaMarshalWithoutRaw(t *testing.T) {
	s := &Schema{Type: TypeObject, Properties: map[string]*Schema{"a": {Type: TypeString}}, Required: []string{"a"}}
	got, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"object","properties":{"a":{"type":"string"}},"required":["a"]}`
	if string(got) != want {
		t.Fatalf("got %s want %s", got, want)
	}
}
