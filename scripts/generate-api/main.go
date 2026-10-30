package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/zhoux77899/router-forge/internal/domain"
)

type schema = map[string]any

func ref(name string) schema { return schema{"$ref": "#/components/schemas/" + name} }

func shape(t reflect.Type) schema {
	if t == reflect.TypeFor[json.RawMessage]() {
		return schema{}
	}
	if t.Kind() == reflect.Pointer {
		return schema{"anyOf": []any{shape(t.Elem()), schema{"type": "null"}}}
	}
	if t.Name() == "Protocol" {
		return schema{"type": "string", "enum": []string{string(domain.ChatCompletions), string(domain.Responses), string(domain.AnthropicMessages)}}
	}
	if t.Kind() == reflect.Struct {
		return ref(t.Name())
	}
	switch t.Kind() {
	case reflect.String:
		return schema{"type": "string"}
	case reflect.Bool:
		return schema{"type": "boolean"}
	case reflect.Int, reflect.Int64:
		return schema{"type": "integer", "format": "int64"}
	case reflect.Float32, reflect.Float64:
		return schema{"type": "number"}
	case reflect.Slice:
		return schema{"type": "array", "items": shape(t.Elem())}
	case reflect.Map:
		return schema{"type": "object", "additionalProperties": shape(t.Elem())}
	}
	panic(t.String())
}

func object(t reflect.Type) schema {
	properties := schema{}
	required := []string{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("json"), ",")
		if len(tag) == 0 || tag[0] == "-" || tag[0] == "" {
			continue
		}
		properties[tag[0]] = shape(f.Type)
		if len(tag) == 1 {
			required = append(required, tag[0])
		}
	}
	return schema{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func main() {
	types := []reflect.Type{
		reflect.TypeFor[domain.Environment](), reflect.TypeFor[domain.ToolStatus](), reflect.TypeFor[domain.Agent](), reflect.TypeFor[domain.Inspection](),
		reflect.TypeFor[domain.Provider](), reflect.TypeFor[domain.ProviderInput](), reflect.TypeFor[domain.Pricing](), reflect.TypeFor[domain.Model](),
		reflect.TypeFor[domain.Suite](), reflect.TypeFor[domain.BenchmarkTask](), reflect.TypeFor[domain.ResolvedBenchmark](), reflect.TypeFor[domain.RunSpec](), reflect.TypeFor[domain.RunSnapshot](), reflect.TypeFor[domain.RuntimeSnapshot](), reflect.TypeFor[domain.ExecutionObservation](),
		reflect.TypeFor[domain.Run](), reflect.TypeFor[domain.Task](), reflect.TypeFor[domain.Attempt](), reflect.TypeFor[domain.JobEvent](),
		reflect.TypeFor[domain.Usage](), reflect.TypeFor[domain.RequestRecord](), reflect.TypeFor[domain.CurrencyTotal](), reflect.TypeFor[domain.RequestSummary](),
	}
	schemas := schema{}
	for _, t := range types {
		schemas[t.Name()] = object(t)
	}
	schemas["Protocol"] = shape(reflect.TypeFor[domain.Protocol]())
	runProperties := schemas["RunSpec"].(schema)["properties"].(schema)
	runProperties["agent_version"] = schema{"type": "string", "deprecated": true, "description": "Historical reports only. Omit for new runs: Harbor uses its default container installation and the actual CLI version is independently observed before inference. Nonempty values in new run requests are rejected."}
	providerInput := schemas["ProviderInput"].(schema)["properties"].(schema)
	providerInput["api_key"] = schema{"type": []string{"string", "null"}, "writeOnly": true, "description": "Stored as plaintext in the private local SQLite database. Omit/null to retain; empty string clears active and historical credential values. Clearing is not cryptographic disk erasure."}
	for _, name := range []string{"Environment", "Model"} {
		copy := object(map[string]reflect.Type{"Environment": reflect.TypeFor[domain.Environment](), "Model": reflect.TypeFor[domain.Model]()}[name])
		r := copy["required"].([]string)
		filtered := []string{}
		for _, key := range r {
			if key != "id" && key != "created_at" {
				filtered = append(filtered, key)
			}
		}
		copy["required"] = filtered
		schemas[name+"Input"] = copy
	}
	for _, name := range []string{"Run", "RequestRecord"} {
		schemas[name+"Page"] = schema{"type": "object", "required": []string{"items", "total"}, "properties": schema{"items": schema{"type": "array", "items": ref(name)}, "total": schema{"type": "integer"}}}
	}
	schemas["Error"] = schema{"type": "object", "required": []string{"error"}, "properties": schema{"error": schema{"type": "string"}}}
	schemas["Status"] = schema{"type": "object", "required": []string{"version", "pid", "running"}, "properties": schema{"version": schema{"type": "string"}, "pid": schema{"type": "integer"}, "running": schema{"type": "boolean"}}}
	schemas["StartInput"] = schema{"type": "object", "required": []string{"confirm_paid"}, "properties": schema{"confirm_paid": schema{"type": "boolean"}}}

	paths := schema{}
	add := func(path, method, id string, input, output schema, admin bool, query []string) {
		op := schema{"operationId": id, "responses": schema{"200": schema{"description": "Success", "content": schema{"application/json": schema{"schema": output}}}, "400": schema{"description": "Invalid configuration", "content": schema{"application/json": schema{"schema": ref("Error")}}}, "401": schema{"description": "Control authentication required"}, "404": schema{"description": "Not found"}, "409": schema{"description": "Invalid state or incompatible environment"}}}
		if admin {
			op["security"] = []any{schema{"controlToken": []string{}}}
		}
		if input != nil {
			op["requestBody"] = schema{"required": true, "content": schema{"application/json": schema{"schema": input}}}
		}
		params := []any{}
		for _, segment := range strings.Split(path, "/") {
			if strings.HasPrefix(segment, "{") {
				params = append(params, schema{"name": strings.Trim(segment, "{}"), "in": "path", "required": true, "schema": schema{"type": "string"}})
			}
		}
		for _, q := range query {
			kind := "string"
			if q == "limit" || q == "offset" || q == "after" {
				kind = "integer"
			}
			params = append(params, schema{"name": q, "in": "query", "required": false, "schema": schema{"type": kind}})
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
		if paths[path] == nil {
			paths[path] = schema{}
		}
		paths[path].(schema)[method] = op
	}
	array := func(name string) schema { return schema{"type": "array", "items": ref(name)} }
	add("/api/status", "get", "getStatus", nil, ref("Status"), false, nil)
	add("/api/environments", "get", "listEnvironments", nil, array("Environment"), true, nil)
	add("/api/environments", "post", "saveEnvironment", ref("EnvironmentInput"), ref("Environment"), true, nil)
	add("/api/environments/{id}/inspect", "post", "inspectEnvironment", schema{"type": "object"}, ref("Inspection"), true, nil)
	add("/api/providers", "get", "listProviders", nil, array("Provider"), true, nil)
	add("/api/providers", "post", "saveProvider", ref("ProviderInput"), ref("Provider"), true, nil)
	add("/api/models", "get", "listModels", nil, array("Model"), true, nil)
	add("/api/models", "post", "saveModel", ref("ModelInput"), ref("Model"), true, nil)
	add("/api/benchmarks", "get", "listBenchmarks", nil, array("Suite"), false, nil)
	add("/api/benchmarks/{id}/tasks", "get", "listBenchmarkTasks", nil, array("BenchmarkTask"), true, []string{"environment_id"})
	add("/api/benchmarks/{id}/resolve", "get", "resolveBenchmark", nil, ref("ResolvedBenchmark"), true, []string{"environment_id"})
	add("/api/runs", "get", "listRuns", nil, ref("RunPage"), false, []string{"limit", "offset"})
	add("/api/runs", "post", "createRun", ref("RunSpec"), ref("Run"), true, nil)
	add("/api/runs/{id}", "get", "getRun", nil, ref("Run"), false, nil)
	add("/api/runs/{id}/start", "post", "startRun", ref("StartInput"), ref("Run"), true, nil)
	add("/api/runs/{id}/cancel", "post", "cancelRun", schema{"type": "object"}, ref("Run"), true, nil)
	add("/api/runs/{id}/events", "get", "listRunEvents", nil, array("JobEvent"), false, []string{"after"})
	add("/api/requests", "get", "listRequests", nil, ref("RequestRecordPage"), false, []string{"run_id", "attempt_id", "limit", "offset"})

	spec := schema{"openapi": "3.1.0", "info": schema{"title": "Router Forge local backend", "version": domain.Version, "description": "Loopback-only read-only results; authenticated Desktop control. Provider keys are write-only and intentionally stored as plaintext in the user's private SQLite database. One explicit task and no implicit paid reruns."}, "servers": []any{schema{"url": "http://127.0.0.1:8787"}}, "paths": paths, "components": schema{"schemas": schemas, "securitySchemes": schema{"controlToken": schema{"type": "http", "scheme": "bearer", "description": "Ephemeral local control token delivered by the native Desktop bridge, not a model provider API key."}}}}
	b, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(filepath.Join("packages", "api", "openapi.json"), append(b, '\n'), 0644); err != nil {
		panic(err)
	}
}
