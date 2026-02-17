package k8s

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ppiankov/clickspectre/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	testingcore "k8s.io/client-go/testing"
)

func TestResolveIP(t *testing.T) {
	cases := []struct {
		name    string
		ip      string
		objects []runtime.Object
		setup   func(client *fake.Clientset)
		want    ServiceInfo
	}{
		{
			name: "pod_ip_resolves_to_service",
			ip:   "10.0.0.1",
			objects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "ns1",
						Labels: map[string]string{
							"app": "demo",
						},
					},
					Status: corev1.PodStatus{PodIP: "10.0.0.1"},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-1",
						Namespace: "ns1",
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{
							"app": "demo",
						},
					},
				},
			},
			want: ServiceInfo{Service: "svc-1", Namespace: "ns1", Pod: "pod-1"},
		},
		{
			name: "ipv6_mapped_ip_is_stripped",
			ip:   "::ffff:10.0.0.2",
			objects: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-2",
						Namespace: "ns2",
						Labels: map[string]string{
							"app": "demo",
						},
					},
					Status: corev1.PodStatus{PodIP: "10.0.0.2"},
				},
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-2",
						Namespace: "ns2",
					},
					Spec: corev1.ServiceSpec{
						Selector: map[string]string{
							"app": "demo",
						},
					},
				},
			},
			want: ServiceInfo{Service: "svc-2", Namespace: "ns2", Pod: "pod-2"},
		},
		{
			name: "service_ip_resolves",
			ip:   "10.0.0.3",
			objects: []runtime.Object{
				&corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "svc-3",
						Namespace: "ns3",
					},
					Spec: corev1.ServiceSpec{
						ClusterIP: "10.0.0.3",
					},
				},
			},
			want: ServiceInfo{Service: "svc-3", Namespace: "ns3", Pod: ""},
		},
		{
			name: "fallback_on_error",
			ip:   "10.0.0.4",
			setup: func(client *fake.Clientset) {
				client.PrependReactor("list", "pods", func(action testingcore.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("boom")
				})
			},
			want: ServiceInfo{Service: "10.0.0.4", Namespace: "", Pod: ""},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tc.objects...)
			if tc.setup != nil {
				tc.setup(client)
			}

			cfg := config.DefaultConfig()
			cfg.Verbose = false

			resolver := &Resolver{
				client:      &Client{clientset: client},
				cache:       NewCache(1 * time.Minute),
				rateLimiter: NewRateLimiter(1000),
				config:      cfg,
			}

			got, err := resolver.ResolveIP(context.Background(), tc.ip)
			if err != nil {
				t.Fatalf("ResolveIP failed: %v", err)
			}
			if got.Service != tc.want.Service || got.Namespace != tc.want.Namespace || got.Pod != tc.want.Pod {
				t.Fatalf("expected %+v, got %+v", tc.want, *got)
			}
		})
	}
}
