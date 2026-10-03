package cluster

import (
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestServedResourcesSkipsMissingCRDs(t *testing.T) {
	client := &fake.FakeDiscovery{Fake: &k8stesting.Fake{Resources: []*metav1.APIResourceList{
		{GroupVersion: "serving.kserve.io/v1beta1", APIResources: []metav1.APIResource{{Name: "inferenceservices"}}},
		{GroupVersion: "serving.kserve.io/v1alpha1", APIResources: []metav1.APIResource{{Name: "servingruntimes"}}},
	}}}
	isvc := schema.GroupVersionResource{Group: "serving.kserve.io", Version: "v1beta1", Resource: "inferenceservices"}
	runtimes := schema.GroupVersionResource{Group: "serving.kserve.io", Version: "v1alpha1", Resource: "servingruntimes"}
	llmisvc := schema.GroupVersionResource{Group: "serving.kserve.io", Version: "v1alpha1", Resource: "llminferenceservices"}
	cron := schema.GroupVersionResource{Group: "argoproj.io", Version: "v1alpha1", Resource: "cronworkflows"}

	served, missing := ServedResources(client, []schema.GroupVersionResource{isvc, runtimes, llmisvc, cron})

	if want := []schema.GroupVersionResource{isvc, runtimes}; !reflect.DeepEqual(served, want) {
		t.Fatalf("served = %v, want %v", served, want)
	}
	if want := []string{"serving.kserve.io/v1alpha1/llminferenceservices", "argoproj.io/v1alpha1/cronworkflows"}; !reflect.DeepEqual(missing, want) {
		t.Fatalf("missing = %v, want %v", missing, want)
	}
}
