package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestNetworkCapabilityFormsHaveEquivalentPermissions(t *testing.T) {
	for _, test := range []struct{ name, legacy, object string }{
		{"hosts", `{"network":["api.example.com"]}`, `{"network":{"hosts":["api.example.com"]}}`},
		{"host approval", `{"network":["api.example.com"],"userHosts":true}`, `{"network":{"hosts":["api.example.com"],"userHosts":true}}`},
		{"empty", `{"network":[]}`, `{"network":{"hosts":[],"userHosts":false}}`},
		{"legacy approval with object hosts", `{"network":[],"userHosts":true}`, `{"network":{"hosts":[]},"userHosts":true}`},
		{"null", `{}`, `{"network":null}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			before, err := permissions(json.RawMessage(test.legacy), nil)
			if err != nil {
				t.Fatal(err)
			}
			after, err := permissions(json.RawMessage(test.object), nil)
			if err != nil {
				t.Fatal(err)
			}
			if diff := diffPermissions(before, after); !diff.Empty() {
				t.Fatalf("equivalent forms changed permissions: %s", diff.markdown(false))
			}
		})
	}
}

func TestNestedNetworkApprovalIsANewPermission(t *testing.T) {
	before, err := permissions(json.RawMessage(`{"network":{"hosts":[]}}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := permissions(json.RawMessage(`{"network":{"hosts":[],"userHosts":true}}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	diff := diffPermissions(before, after)
	if len(diff.Added) != 1 || diff.Added[0].Key != "network:userHosts" || len(diff.Removed) != 0 || len(diff.Changed) != 0 {
		t.Fatalf("wrong approval diff: %#v", diff)
	}
	if !strings.Contains(diff.markdown(false), "ask an administrator to approve more hosts") {
		t.Fatal("permission summary does not explain administrator approval")
	}
	both, err := permissions(json.RawMessage(`{"userHosts":true,"network":{"userHosts":true}}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if diff := diffPermissions(after, both); !diff.Empty() {
		t.Fatalf("approval was duplicated: %s", diff.markdown(false))
	}
}

func TestNetworkCapabilityRejectsInvalidForms(t *testing.T) {
	for _, network := range []string{`"api.example.com"`, `true`, `42`, `[1]`, `{"hosts":"api.example.com"}`, `{"hosts":[false]}`, `{"userHosts":"true"}`} {
		if _, err := permissions(json.RawMessage(`{"network":`+network+`}`), nil); err == nil {
			t.Errorf("accepted invalid network capability %s", network)
		}
	}
}
