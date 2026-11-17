package kube

import (
	"strings"
	"testing"
)

const secretTable = `{
 "kind": "Table", "apiVersion": "meta.k8s.io/v1", "metadata": {"continue": "next-page"},
 "columnDefinitions": [{"name": "Name"}, {"name": "Type"}, {"name": "Data"}, {"name": "Age"}],
 "rows": [{
   "cells": ["api-db", "Opaque", 2, "10d"],
   "object": {"kind": "PartialObjectMetadata", "apiVersion": "meta.k8s.io/v1",
     "metadata": {"name": "api-db", "namespace": "payments", "uid": "u1",
       "creationTimestamp": "2025-01-10T00:00:00Z",
       "annotations": {"kubectl.kubernetes.io/last-applied-configuration": "{\"data\":{\"p\":\"LEAK\"}}"}}}
 }]
}`

func TestParseSecretTable(t *testing.T) {
	got, next, err := ParseSecretTable([]byte(secretTable))
	if err != nil {
		t.Fatal(err)
	}
	if next != "next-page" || len(got) != 1 {
		t.Fatalf("next=%q rows=%d", next, len(got))
	}
	s := got[0]
	if s.Name != "api-db" || s.NS != "payments" || s.Type != "Opaque" || s.Keys != 2 || s.UID != "u1" {
		t.Fatalf("row = %+v", s)
	}
}

func TestParseSecretTableRefusesFullObjects(t *testing.T) {
	full := strings.Replace(secretTable, `"kind": "PartialObjectMetadata"`, `"kind": "Secret"`, 1)
	if _, _, err := ParseSecretTable([]byte(full)); err == nil {
		t.Fatal("want error when the server sends full Secret objects")
	}
	if _, _, err := ParseSecretTable([]byte(`{"kind":"SecretList","items":[]}`)); err == nil {
		t.Fatal("want error when the response is not a Table")
	}
}
