package projects

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestSelectBCHNamespacesFromProjectFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/projects.json")
	if err != nil {
		t.Fatal(err)
	}
	var projects map[string]json.RawMessage
	if err := json.Unmarshal(data, &projects); err != nil {
		t.Fatal(err)
	}

	got, err := selectBCHNamespaces(projects)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"payments-bch", "retail-bch"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected namespaces = %v, want %v", got, want)
	}
}

func TestSelectBCHNamespacesRejectsMalformedServing(t *testing.T) {
	projects := map[string]json.RawMessage{
		"broken": json.RawMessage(`{"serving":"BCH"}`),
	}
	if _, err := selectBCHNamespaces(projects); err == nil {
		t.Fatal("expected malformed serving value to be rejected")
	}
}

func TestParseProjectImageIDsReadsEveryBatchDeploy(t *testing.T) {
	values := []byte(`project:
  batchDeploys:
    - imageId: ald7383jdls8373
    - imageId: image-2
`)

	got, err := parseProjectImageIDs(values)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"ald7383jdls8373", "image-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("image IDs = %v, want %v", got, want)
	}
}
