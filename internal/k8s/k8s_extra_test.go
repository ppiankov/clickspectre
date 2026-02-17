package k8s

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ppiankov/clickspectre/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func newTestResolver(objects ...runtime.Object) *Resolver {
	cfg := config.DefaultConfig()
	cfg.Verbose = false

	return &Resolver{
		client:      &Client{clientset: fake.NewSimpleClientset(objects...)},
		cache:       NewCache(time.Minute),
		rateLimiter: NewRateLimiter(1000),
		config:      cfg,
	}
}

func TestCacheSetGetClearAndSize(t *testing.T) {
	cache := NewCache(time.Minute)

	cache.Set("10.0.0.1", &ServiceInfo{Service: "svc", Namespace: "ns", Pod: "pod"})
	if got := cache.Get("10.0.0.1"); got == nil || got.Service != "svc" {
		t.Fatalf("expected cached service info, got %+v", got)
	}
	if cache.Size() != 1 {
		t.Fatalf("expected cache size 1, got %d", cache.Size())
	}

	cache.Clear()
	if cache.Size() != 0 {
		t.Fatalf("expected cache size 0 after clear, got %d", cache.Size())
	}
}

func TestCacheGetExpiredAndEviction(t *testing.T) {
	cache := NewCache(time.Minute)
	cache.maxSize = 3

	cache.entries["expired"] = &CacheEntry{
		Info:      &ServiceInfo{Service: "old"},
		ExpiresAt: time.Now().Add(-time.Second),
	}
	cache.entries["a"] = &CacheEntry{
		Info:      &ServiceInfo{Service: "a"},
		ExpiresAt: time.Now().Add(time.Minute),
	}
	cache.entries["b"] = &CacheEntry{
		Info:      &ServiceInfo{Service: "b"},
		ExpiresAt: time.Now().Add(time.Minute),
	}

	cache.Set("c", &ServiceInfo{Service: "c"})

	if got := cache.Get("expired"); got != nil {
		t.Fatalf("expected expired entry to be gone, got %+v", got)
	}
	if cache.Size() > cache.maxSize {
		t.Fatalf("expected cache size <= %d, got %d", cache.maxSize, cache.Size())
	}
}

func TestRateLimiterAllow(t *testing.T) {
	limiter := NewRateLimiter(1)

	if !limiter.Allow() {
		t.Fatal("expected initial Allow to return true")
	}

	// Burst is 2 for rps=1, so the third immediate call should be denied.
	_ = limiter.Allow()
	if limiter.Allow() {
		t.Fatal("expected third immediate Allow to return false")
	}
}

func TestNewClientAndNewResolverErrorPaths(t *testing.T) {
	_, err := NewClient("/definitely-missing-kubeconfig")
	if err == nil || !strings.Contains(err.Error(), "failed to load kubeconfig") {
		t.Fatalf("expected kubeconfig load error, got %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.KubeConfig = "/definitely-missing-kubeconfig"
	_, err = NewResolver(cfg)
	if err == nil || !strings.Contains(err.Error(), "failed to create Kubernetes client") {
		t.Fatalf("expected resolver init error, got %v", err)
	}
}

func TestResolveIPCacheHitAndClose(t *testing.T) {
	resolver := newTestResolver()
	want := &ServiceInfo{Service: "cached", Namespace: "ns", Pod: "pod"}
	resolver.cache.Set("10.0.0.9", want)

	got, err := resolver.ResolveIP(context.Background(), "10.0.0.9")
	if err != nil {
		t.Fatalf("ResolveIP failed: %v", err)
	}
	if got.Service != want.Service || got.Namespace != want.Namespace || got.Pod != want.Pod {
		t.Fatalf("expected cached value %+v, got %+v", want, got)
	}

	if err := resolver.Close(); err != nil {
		t.Fatalf("expected Close nil error, got %v", err)
	}
}

func TestResolvePodToServiceFallbackToPodName(t *testing.T) {
	resolver := newTestResolver()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-only",
			Namespace: "ns1",
			Labels:    map[string]string{},
		},
	}

	info, err := resolver.resolvePodToService(context.Background(), pod)
	if err != nil {
		t.Fatalf("resolvePodToService failed: %v", err)
	}
	if info.Service != "pod-only" || info.Namespace != "ns1" || info.Pod != "pod-only" {
		t.Fatalf("unexpected fallback info: %+v", info)
	}
}

func TestFindServiceByIPVariantsAndMiss(t *testing.T) {
	resolver := newTestResolver(
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc-cluster", Namespace: "ns1"},
			Spec:       corev1.ServiceSpec{ClusterIP: "10.0.0.10"},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc-lb", Namespace: "ns2"},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.11"}},
				},
			},
		},
		&corev1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: "svc-ext", Namespace: "ns3"},
			Spec:       corev1.ServiceSpec{ExternalIPs: []string{"10.0.0.12"}},
		},
	)

	cluster, err := resolver.findServiceByIP(context.Background(), "10.0.0.10")
	if err != nil || cluster.Service != "svc-cluster" {
		t.Fatalf("expected clusterIP match, got info=%+v err=%v", cluster, err)
	}

	lb, err := resolver.findServiceByIP(context.Background(), "10.0.0.11")
	if err != nil || lb.Service != "svc-lb" {
		t.Fatalf("expected load balancer IP match, got info=%+v err=%v", lb, err)
	}

	ext, err := resolver.findServiceByIP(context.Background(), "10.0.0.12")
	if err != nil || ext.Service != "svc-ext" {
		t.Fatalf("expected external IP match, got info=%+v err=%v", ext, err)
	}

	_, err = resolver.findServiceByIP(context.Background(), "10.0.0.250")
	if err == nil || !strings.Contains(err.Error(), "no service found with IP") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

func TestMatchesSelector(t *testing.T) {
	if matchesSelector(map[string]string{"app": "a"}, map[string]string{}) {
		t.Fatal("expected empty selector to return false")
	}
	if matchesSelector(map[string]string{"app": "a"}, map[string]string{"app": "b"}) {
		t.Fatal("expected mismatched selector to return false")
	}
	if !matchesSelector(map[string]string{"app": "a", "tier": "api"}, map[string]string{"app": "a"}) {
		t.Fatal("expected matching selector to return true")
	}
}
