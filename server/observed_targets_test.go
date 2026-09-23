package server

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestConvertExtractsOnlyStructuredObservedTargets(t *testing.T) {
	for _, sample := range []struct {
		format string
		body   string
		want   []string
	}{
		{"txt", "api.example.com\nlogin.example.com\nlogin.example.com\nscan login.example.com now\n", []string{"host:api.example.com", "host:login.example.com"}},
		{"csv", "host,status\napi.example.com,200\nlogin.example.com,401\n", []string{"host:api.example.com", "host:login.example.com"}},
		{"jsonl", "{\"host\":\"login.example.com\",\"note\":\"secret.example.com\"}\n", []string{"host:login.example.com"}},
		{"md", "# Notes about login.example.com\n", nil},
	} {
		args := []string{"-", "--format=" + sample.format, "--program=acme", "--platform=h1", "--target=API Service", "--classification=internal", "--source=fixture"}
		opts, err := parseConvertArgs(args)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := runConvert(context.Background(), opts, strings.NewReader(sample.body))
		if err != nil {
			t.Fatalf("%s: %v", sample.format, err)
		}
		var doc ConvertedDocument
		if err := json.Unmarshal(encoded, &doc); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(doc.ObservedTargets, sample.want) && !(len(doc.ObservedTargets) == 0 && len(sample.want) == 0) {
			t.Fatalf("%s extracted %v, want %v", sample.format, doc.ObservedTargets, sample.want)
		}
	}
}

func TestObservedTargetsRequireRegistrationAndCanonicalAssets(t *testing.T) {
	for _, content := range []string{
		`{"observed_targets":["host:api.example.com"]}`,
		`{"platform":"h1","target_name":"API Service","observed_targets":["host:API.Example.COM"]}`,
		`{"platform":"h1","target_name":"API Service","observed_targets":["wildcard_domain:*.example.com"]}`,
	} {
		if _, err := extractReconMetadata("programs/acme/source.json", "acme", []byte(content)); err == nil {
			t.Fatalf("accepted invalid observation metadata: %s", content)
		}
	}
}
