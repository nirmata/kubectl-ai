// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
