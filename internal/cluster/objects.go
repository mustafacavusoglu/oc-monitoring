package cluster

import (
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Pods is the core v1 pod resource, which unlike the CRDs never changes version.
var Pods = schema.GroupVersionResource{Version: "v1", Resource: "pods"}

// NestedTime reads an RFC 3339 timestamp field; missing or malformed values are nil.
func NestedTime(object map[string]any, path ...string) *time.Time {
	value, _, _ := unstructured.NestedString(object, path...)
	return ParseTime(value)
}

func ParseTime(value string) *time.Time {
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}

func CreationTime(object *unstructured.Unstructured) *time.Time {
	created := object.GetCreationTimestamp().Time
	if created.IsZero() {
		return nil
	}
	created = created.UTC()
	return &created
}

// NestedInt reads an integer field that may be decoded as int64 (API server)
// or float64 (encoding/json).
func NestedInt(object map[string]any, path ...string) *int64 {
	value, found, _ := unstructured.NestedFieldNoCopy(object, path...)
	if !found {
		return nil
	}
	var n int64
	switch v := value.(type) {
	case int64:
		n = v
	case float64:
		n = int64(v)
	default:
		return nil
	}
	return &n
}

// Key identifies a namespaced object.
func Key(namespace, name string) string {
	return namespace + "/" + name
}
