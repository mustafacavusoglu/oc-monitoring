package cluster

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"monitor/internal/model"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8swatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

var watchedResources = []schema.GroupVersionResource{
	{Group: "argoproj.io", Version: "v1alpha1", Resource: "cronworkflows"},
	{Group: "argoproj.io", Version: "v1alpha1", Resource: "workflows"},
	{Version: "v1", Resource: "pods"},
}

type Snapshot struct {
	CronWorkflows []*unstructured.Unstructured
	Workflows     []*unstructured.Unstructured
	Pods          []*unstructured.Unstructured
	Health        model.SourceHealth
}

type namespaceWatch struct {
	ctx            context.Context
	cancel         context.CancelFunc
	informers      []cache.SharedIndexInformer
	errors         map[string]string
	syncedResource map[string]bool
}

type Watcher struct {
	client     dynamic.Interface
	namespaces func() []string

	mu          sync.RWMutex
	watches     map[string]*namespaceWatch
	lastSuccess *time.Time
	wasReady    bool
	lastStatus  time.Time
}

func NewWatcher(cfg *rest.Config, namespaces func() []string) (*Watcher, error) {
	if cfg == nil {
		var err error
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("load in-cluster Kubernetes config: %w", err)
		}
	}
	client, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes dynamic client: %w", err)
	}
	if namespaces == nil {
		namespaces = func() []string { return nil }
	}
	return &Watcher{client: client, namespaces: namespaces, watches: make(map[string]*namespaceWatch)}, nil
}

func (w *Watcher) Run(ctx context.Context) {
	log.Printf("cluster watcher starting")
	w.reconcile(ctx)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			w.stopAll()
			return
		case <-ticker.C:
			w.reconcile(ctx)
		}
	}
}

func (w *Watcher) reconcile(ctx context.Context) {
	desired := normalizedNamespaces(w.namespaces())
	desiredSet := make(map[string]struct{}, len(desired))
	for _, namespace := range desired {
		desiredSet[namespace] = struct{}{}
	}

	var startWatches []*namespaceWatch
	watchSetChanged := false
	w.mu.Lock()
	for namespace, watch := range w.watches {
		if _, found := desiredSet[namespace]; !found {
			log.Printf("cluster watch stopping: namespace=%q", namespace)
			watch.cancel()
			delete(w.watches, namespace)
			watchSetChanged = true
		}
	}
	for _, namespace := range desired {
		if _, found := w.watches[namespace]; !found {
			watch := w.newNamespaceWatch(ctx, namespace)
			w.watches[namespace] = watch
			startWatches = append(startWatches, watch)
			watchSetChanged = true
		}
	}
	allSynced := true
	var watchErrors []string
	for namespace, watch := range w.watches {
		for i, informer := range watch.informers {
			if !informer.HasSynced() {
				allSynced = false
				continue
			}
			resource := watchedResources[i].Resource
			if !watch.syncedResource[resource] {
				watch.syncedResource[resource] = true
				log.Printf("cluster informer synced: namespace=%q resource=%q objects=%d", namespace, resource, len(informer.GetStore().List()))
			}
		}
		for resource, watchError := range watch.errors {
			watchErrors = append(watchErrors, namespace+"/"+resource+": "+watchError)
		}
	}
	sort.Strings(watchErrors)
	if time.Since(w.lastStatus) >= 30*time.Second || w.lastStatus.IsZero() {
		counts := map[string]int{"cronworkflows": 0, "workflows": 0, "pods": 0}
		synced := 0
		for _, watch := range w.watches {
			for i, informer := range watch.informers {
				resource := watchedResources[i].Resource
				counts[resource] += len(informer.GetStore().List())
				if informer.HasSynced() {
					synced++
				}
			}
		}
		log.Printf("cluster watcher status: namespaces=%d informers_synced=%d/%d cronworkflows=%d workflows=%d pods=%d errors=%q", len(w.watches), synced, len(w.watches)*len(watchedResources), counts["cronworkflows"], counts["workflows"], counts["pods"], watchErrors)
		w.lastStatus = time.Now()
	}
	ready := allSynced && len(watchErrors) == 0
	if ready && (!w.wasReady || w.lastSuccess == nil || watchSetChanged) {
		now := time.Now().UTC()
		w.lastSuccess = &now
	}
	w.wasReady = ready
	w.mu.Unlock()
	for _, watch := range startWatches {
		for _, informer := range watch.informers {
			go informer.Run(watch.ctx.Done())
		}
	}
}

func (w *Watcher) newNamespaceWatch(parent context.Context, namespace string) *namespaceWatch {
	ctx, cancel := context.WithCancel(parent)
	watch := &namespaceWatch{ctx: ctx, cancel: cancel, errors: make(map[string]string), syncedResource: make(map[string]bool)}
	for _, resource := range watchedResources {
		log.Printf("cluster watch starting: namespace=%q resource=%q", namespace, resource.Resource)
		namespacedResource := w.client.Resource(resource).Namespace(namespace)
		listWatch := &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return namespacedResource.List(ctx, options)
			},
			WatchFunc: func(options metav1.ListOptions) (k8swatch.Interface, error) {
				return namespacedResource.Watch(ctx, options)
			},
		}
		informer := cache.NewSharedIndexInformer(
			listWatch,
			&unstructured.Unstructured{},
			0,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		)
		_ = informer.SetWatchErrorHandler(func(_ *cache.Reflector, err error) {
			w.setWatchError(namespace, watch, resource.Resource, err)
		})
		_, _ = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(any) { w.clearWatchError(namespace, watch, resource.Resource) },
			UpdateFunc: func(any, any) {
				w.clearWatchError(namespace, watch, resource.Resource)
			},
			DeleteFunc: func(any) { w.clearWatchError(namespace, watch, resource.Resource) },
		})
		watch.informers = append(watch.informers, informer)
	}
	return watch
}

func (w *Watcher) setWatchError(namespace string, target *namespaceWatch, resource string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if watch := w.watches[namespace]; watch == target {
		message := err.Error()
		if watch.errors[resource] != message {
			log.Printf("cluster watch error: namespace=%q resource=%q error=%q", namespace, resource, message)
		}
		watch.errors[resource] = message
	}
}

func (w *Watcher) clearWatchError(namespace string, target *namespaceWatch, resource string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if watch := w.watches[namespace]; watch == target {
		if _, found := watch.errors[resource]; found {
			log.Printf("cluster watch recovered: namespace=%q resource=%q", namespace, resource)
			delete(watch.errors, resource)
		}
	}
}

func (w *Watcher) stopAll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for namespace, watch := range w.watches {
		watch.cancel()
		delete(w.watches, namespace)
	}
}

func (w *Watcher) Snapshot() Snapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()

	snapshot := Snapshot{}
	allSynced := true
	var watchErrors []string
	namespaces := make([]string, 0, len(w.watches))
	for namespace := range w.watches {
		namespaces = append(namespaces, namespace)
	}
	sort.Strings(namespaces)

	for _, namespace := range namespaces {
		watch := w.watches[namespace]
		for i, informer := range watch.informers {
			if !informer.HasSynced() {
				allSynced = false
			}
			for _, item := range informer.GetStore().List() {
				resource, ok := item.(*unstructured.Unstructured)
				if !ok {
					continue
				}
				copy := resource.DeepCopy()
				switch watchedResources[i].Resource {
				case "cronworkflows":
					snapshot.CronWorkflows = append(snapshot.CronWorkflows, copy)
				case "workflows":
					snapshot.Workflows = append(snapshot.Workflows, copy)
				case "pods":
					snapshot.Pods = append(snapshot.Pods, copy)
				}
			}
		}
		for resource, watchError := range watch.errors {
			watchErrors = append(watchErrors, namespace+"/"+resource+": "+watchError)
		}
	}

	sort.Strings(watchErrors)
	state := "ready"
	errorText := ""
	if len(watchErrors) != 0 {
		state = "degraded"
		errorText = strings.Join(watchErrors, "; ")
	} else if !allSynced {
		state = "syncing"
		errorText = "waiting for informer cache sync"
	}
	if w.lastSuccess != nil {
		lastSuccess := *w.lastSuccess
		snapshot.Health.LastSuccess = &lastSuccess
	}
	if len(w.watches) == 0 && allSynced {
		snapshot.Health.State = "ready"
	} else {
		snapshot.Health.State = state
	}
	snapshot.Health.Error = errorText
	return snapshot
}

func normalizedNamespaces(namespaces []string) []string {
	seen := make(map[string]struct{}, len(namespaces))
	result := make([]string, 0, len(namespaces))
	for _, namespace := range namespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			continue
		}
		if _, found := seen[namespace]; found {
			continue
		}
		seen[namespace] = struct{}{}
		result = append(result, namespace)
	}
	sort.Strings(result)
	return result
}
