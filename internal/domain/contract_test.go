package domain

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOpenAPIIsNativeContractAndWriteOnlyCredentials(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "packages", "api", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		OpenAPI    string `json:"openapi"`
		Components struct {
			Schemas map[string]struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err = json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.OpenAPI != "3.1.0" {
		t.Fatal(spec.OpenAPI)
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[Provider](), reflect.TypeFor[Model](), reflect.TypeFor[Environment](), reflect.TypeFor[RunSpec](), reflect.TypeFor[Usage](), reflect.TypeFor[RequestRecord]()} {
		schema, ok := spec.Components.Schemas[typ.Name()]
		if !ok {
			t.Fatal("missing schema", typ.Name())
		}
		for i := 0; i < typ.NumField(); i++ {
			field := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			if field == "-" {
				continue
			}
			if _, ok := schema.Properties[field]; !ok {
				t.Fatal(typ.Name(), field)
			}
		}
	}
	if _, exists := spec.Components.Schemas["Provider"].Properties["api_key"]; exists {
		t.Fatal("credential in public provider")
	}
	var key struct {
		WriteOnly bool `json:"writeOnly"`
	}
	json.Unmarshal(spec.Components.Schemas["ProviderInput"].Properties["api_key"], &key)
	if !key.WriteOnly {
		t.Fatal("API key isn't declared write-only")
	}
	var agentVersion struct {
		Deprecated bool `json:"deprecated"`
	}
	if err := json.Unmarshal(spec.Components.Schemas["RunSpec"].Properties["agent_version"], &agentVersion); err != nil {
		t.Fatal(err)
	}
	if !agentVersion.Deprecated {
		t.Fatal("legacy agent version request must be marked deprecated")
	}
}
