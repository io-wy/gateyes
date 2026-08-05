package main

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"
)

type eventSource interface {
	Start(context.Context) (<-chan struct{}, error)
}

type kubernetesEventSource struct {
	client    dynamic.Interface
	namespace string
	resync    time.Duration
}

func newKubernetesEventSource(client dynamic.Interface, namespace string, resync time.Duration) *kubernetesEventSource {
	return &kubernetesEventSource{client: client, namespace: namespace, resync: resync}
}

func (s *kubernetesEventSource) Start(ctx context.Context) (<-chan struct{}, error) {
	events := make(chan struct{}, 1)
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(s.client, s.resync, s.namespace, nil)
	for _, gvr := range watchedGVRs() {
		informer := factory.ForResource(gvr).Informer()
		if _, err := informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: func(any) {
				notify(events)
			},
			UpdateFunc: func(any, any) {
				notify(events)
			},
			DeleteFunc: func(any) {
				notify(events)
			},
		}); err != nil {
			return nil, err
		}
	}
	factory.Start(ctx.Done())
	return events, nil
}

func watchedGVRs() []schema.GroupVersionResource {
	return []schema.GroupVersionResource{
		modelEndpointGVR,
		routePolicyGVR,
		budgetPolicyGVR,
		autoscaleGVR,
		inferenceSvcGVR,
	}
}

func notify(events chan<- struct{}) {
	select {
	case events <- struct{}{}:
	default:
	}
}

func objectNamespace(obj any) string {
	if item, ok := obj.(*unstructured.Unstructured); ok {
		return item.GetNamespace()
	}
	if tombstone, ok := obj.(cache.DeletedFinalStateUnknown); ok {
		if item, ok := tombstone.Obj.(*unstructured.Unstructured); ok {
			return item.GetNamespace()
		}
	}
	return metav1.NamespaceAll
}
