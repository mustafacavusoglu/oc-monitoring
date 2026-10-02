// Package testutil holds helpers shared by package tests.
package testutil

import (
	"encoding/json"
	"os"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// LoadObjects reads a JSON array of Kubernetes objects from a fixture file.
func LoadObjects(t *testing.T, file string) []*unstructured.Unstructured {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var objects []map[string]any
	if err := json.Unmarshal(data, &objects); err != nil {
		t.Fatal(err)
	}
	result := make([]*unstructured.Unstructured, 0, len(objects))
	for _, object := range objects {
		result = append(result, &unstructured.Unstructured{Object: object})
	}
	return result
}
