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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8swatch "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	reconcileInterval = 2 * time.Second
	statusLogInterval = 30 * time.Second
	lastAppliedAnno   = "kubectl.kubernetes.io/last-applied-configuration"
)

// AllNamespaces makes a Watcher list/watch its resources cluster-wide with one
// informer per resource.
func AllNamespaces() []string { return []string{metav1.NamespaceAll} }

// Snapshot exposes objects straight from the informer caches. They are shared
// with the informers and must be treated as read-only.
type Snapshot struct {
	Objects map[schema.GroupVersionResource][]*unstructured.Unstructured
	Health  model.SourceHealth
}

type namespaceWatch struct {
	done      <-chan struct{}
	cancel    context.CancelFunc
	informers map[schema.GroupVersionResource]cache.SharedIndexInformer
	errors    map[schema.GroupVersionResource]string
	logged    map[schema.GroupVersionResource]bool
}

// Watcher keeps informer caches for a fixed set of resources across a
// namespace set that may change over time.
type Watcher struct {
	name       string
	client     dynamic.Interface
	resources  []schema.GroupVersionResource
	missing    []string // configured resources the API server does not serve
	namespaces func() []string

	mu            sync.RWMutex
	watches       map[string]*namespaceWatch
	lastSuccess   *time.Time
	wasReady      bool
	lastStatusLog time.Time
}

type Clients struct {
	Dynamic   dynamic.Interface
	Discovery discovery.DiscoveryInterface
}

func NewClients() (Clients, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		return Clients{}, fmt.Errorf("load in-cluster Kubernetes config: %w", err)
	}
	dynamicClient, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return Clients{}, fmt.Errorf("create Kubernetes dynamic client: %w", err)
	}
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return Clients{}, fmt.Errorf("create Kubernetes discovery client: %w", err)
	}
	return Clients{Dynamic: dynamicClient, Discovery: discoveryClient}, nil
}

// NewWatcher watches only the resources the API server serves. A configured
// resource whose CRD is not installed (or has another version) is skipped and
// reported in the health message instead of failing its informer forever.
func NewWatcher(name string, clients Clients, resources []schema.GroupVersionResource, namespaces func() []string) *Watcher {
	served, missing := ServedResources(clients.Discovery, resources)
	for _, resource := range missing {
		log.Printf("%s watcher skipping resource not served by the cluster: %s (check the CRD and its version in the ConfigMap; restart after installing it)", name, resource)
	}
	return &Watcher{name: name, client: clients.Dynamic, resources: served, missing: missing, namespaces: namespaces, watches: make(map[string]*namespaceWatch)}
}

// ServedResources splits resources into those the API server serves and the
// "group/version/resource" names of those it does not. Discovery failures
// other than "not found" keep the resource so a transient error hides nothing.
func ServedResources(client discovery.DiscoveryInterface, resources []schema.GroupVersionResource) ([]schema.GroupVersionResource, []string) {
	var served []schema.GroupVersionResource
	var missing []string
	for _, gvr := range resources {
		list, err := client.ServerResourcesForGroupVersion(gvr.GroupVersion().String())
		switch {
		case apierrors.IsNotFound(err):
			missing = append(missing, resourceName(gvr))
		case err != nil:
			log.Printf("resource discovery failed, watching anyway: resource=%s error=%v", resourceName(gvr), err)
			served = append(served, gvr)
		case hasResource(list, gvr.Resource):
			served = append(served, gvr)
		default:
			missing = append(missing, resourceName(gvr))
		}
	}
	return served, missing
}

func hasResource(list *metav1.APIResourceList, resource string) bool {
	for _, item := range list.APIResources {
		if item.Name == resource {
			return true
		}
	}
	return false
}

func resourceName(gvr schema.GroupVersionResource) string {
	return gvr.Group + "/" + gvr.Version + "/" + gvr.Resource
}

func (w *Watcher) Run(ctx context.Context) {
	log.Printf("%s watcher starting: resources=%v", w.name, w.resources)
	ticker := time.NewTicker(reconcileInterval)
	defer ticker.Stop()
	for {
		w.reconcile(ctx)
		select {
		case <-ctx.Done():
			w.stopAll()
			return
		case <-ticker.C:
		}
	}
}

func (w *Watcher) reconcile(ctx context.Context) {
	desired := make(map[string]struct{})
	for _, namespace := range w.namespaces() {
		desired[namespace] = struct{}{}
	}

	var started []*namespaceWatch
	changed := false
	w.mu.Lock()
	for namespace, watch := range w.watches {
		if _, keep := desired[namespace]; !keep {
			log.Printf("%s watch stopping: namespace=%q", w.name, namespace)
			watch.cancel()
			delete(w.watches, namespace)
			changed = true
		}
	}
	for namespace := range desired {
		if _, found := w.watches[namespace]; !found {
			watch := w.newNamespaceWatch(ctx, namespace)
			w.watches[namespace] = watch
			started = append(started, watch)
			changed = true
		}
	}
	w.logSyncedInformers()
	synced, errs := w.status()
	if time.Since(w.lastStatusLog) >= statusLogInterval {
		log.Printf("%s watcher status: namespaces=%d objects=%v synced=%v errors=%q", w.name, len(w.watches), w.objectCounts(), synced, errs)
		w.lastStatusLog = time.Now()
	}
	ready := synced && len(errs) == 0
	if ready && (!w.wasReady || w.lastSuccess == nil || changed) {
		now := time.Now().UTC()
		w.lastSuccess = &now
	}
	w.wasReady = ready
	w.mu.Unlock()

	for _, watch := range started {
		for _, informer := range watch.informers {
			go informer.Run(watch.done)
		}
	}
}

func (w *Watcher) newNamespaceWatch(parent context.Context, namespace string) *namespaceWatch {
	ctx, cancel := context.WithCancel(parent)
	watch := &namespaceWatch{
		done:      ctx.Done(),
		cancel:    cancel,
		informers: make(map[schema.GroupVersionResource]cache.SharedIndexInformer, len(w.resources)),
		errors:    make(map[schema.GroupVersionResource]string),
		logged:    make(map[schema.GroupVersionResource]bool),
	}
	for _, gvr := range w.resources {
		log.Printf("%s watch starting: namespace=%q resource=%q", w.name, namespace, gvr.String())
		client := w.client.Resource(gvr).Namespace(namespace)
		informer := cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
					return client.List(ctx, options)
				},
				WatchFunc: func(options metav1.ListOptions) (k8swatch.Interface, error) {
					return client.Watch(ctx, options)
				},
			},
			&unstructured.Unstructured{},
			0,
			cache.Indexers{},
		)
		// Dropping bulky metadata keeps the cache small; the dashboard never reads it.
		_ = informer.SetTransform(trimMetadata)
		_ = informer.SetWatchErrorHandler(func(_ *cache.Reflector, err error) {
			w.setWatchError(namespace, watch, gvr, err.Error())
		})
		clear := func() { w.setWatchError(namespace, watch, gvr, "") }
		_, _ = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    func(any) { clear() },
			UpdateFunc: func(any, any) { clear() },
			DeleteFunc: func(any) { clear() },
		})
		watch.informers[gvr] = informer
	}
	return watch
}

func trimMetadata(obj any) (any, error) {
	if object, ok := obj.(*unstructured.Unstructured); ok {
		object.SetManagedFields(nil)
		if annotations := object.GetAnnotations(); annotations[lastAppliedAnno] != "" {
			delete(annotations, lastAppliedAnno)
			object.SetAnnotations(annotations)
		}
	}
	return obj, nil
}

// setWatchError records (or clears, when message is empty) a watch error for
// the namespace watch that is still current.
func (w *Watcher) setWatchError(namespace string, target *namespaceWatch, gvr schema.GroupVersionResource, message string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.watches[namespace] != target {
		return
	}
	previous, found := target.errors[gvr]
	switch {
	case message == "" && found:
		log.Printf("%s watch recovered: namespace=%q resource=%q", w.name, namespace, gvr.Resource)
		delete(target.errors, gvr)
	case message != "" && previous != message:
		log.Printf("%s watch error: namespace=%q resource=%q error=%q", w.name, namespace, gvr.Resource, message)
		target.errors[gvr] = message
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
	objects := make(map[schema.GroupVersionResource][]*unstructured.Unstructured, len(w.resources))
	for _, watch := range w.watches {
		for gvr, informer := range watch.informers {
			for _, item := range informer.GetStore().List() {
				if object, ok := item.(*unstructured.Unstructured); ok {
					objects[gvr] = append(objects[gvr], object)
				}
			}
		}
	}

	health := model.SourceHealth{State: model.StateReady}
	if w.lastSuccess != nil {
		lastSuccess := *w.lastSuccess
		health.LastSuccess = &lastSuccess
	}
	synced, errs := w.status()
	switch {
	case len(errs) > 0:
		health.State, health.Error = model.StateDegraded, strings.Join(errs, "; ")
	case !synced:
		health.State, health.Error = model.StateSyncing, "waiting for informer cache sync"
	case len(w.missing) > 0:
		health.Error = "not served by the cluster, skipped: " + strings.Join(w.missing, ", ")
	}
	return Snapshot{Objects: objects, Health: health}
}

// status must be called with w.mu held.
func (w *Watcher) status() (bool, []string) {
	synced := true
	var errs []string
	for namespace, watch := range w.watches {
		for gvr, informer := range watch.informers {
			synced = synced && informer.HasSynced()
			if message := watch.errors[gvr]; message != "" {
				errs = append(errs, fmt.Sprintf("%s/%s: %s", displayNamespace(namespace), gvr.Resource, message))
			}
		}
	}
	sort.Strings(errs)
	return synced, errs
}

// logSyncedInformers must be called with w.mu held for writing.
func (w *Watcher) logSyncedInformers() {
	for namespace, watch := range w.watches {
		for gvr, informer := range watch.informers {
			if informer.HasSynced() && !watch.logged[gvr] {
				watch.logged[gvr] = true
				log.Printf("%s informer synced: namespace=%q resource=%q objects=%d", w.name, displayNamespace(namespace), gvr.Resource, len(informer.GetStore().ListKeys()))
			}
		}
	}
}

// objectCounts must be called with w.mu held.
func (w *Watcher) objectCounts() map[string]int {
	counts := make(map[string]int, len(w.resources))
	for _, watch := range w.watches {
		for gvr, informer := range watch.informers {
			counts[gvr.Resource] += len(informer.GetStore().ListKeys())
		}
	}
	return counts
}

func displayNamespace(namespace string) string {
	if namespace == metav1.NamespaceAll {
		return "*"
	}
	return namespace
}
