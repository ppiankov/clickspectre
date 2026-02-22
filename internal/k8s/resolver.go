package k8s

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/ppiankov/clickspectre/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// K8sResolverInterface defines the interface for Kubernetes IP resolution
type K8sResolverInterface interface {
	ResolveIP(ctx context.Context, ip string) (*ServiceInfo, error)
	Close() error
}

// ErrNotFound is returned when no K8s service or pod is found for an IP
var ErrNotFound = fmt.Errorf("no pod or service found for IP")

// Resolver resolves IP addresses to Kubernetes services
type Resolver struct {
	client      *Client
	cache       *Cache
	rateLimiter *RateLimiter
	config      *config.Config
}

// NewResolver creates a new IP→Service resolver
func NewResolver(cfg *config.Config) (*Resolver, error) {
	client, err := NewClient(cfg.KubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	cache := NewCache(cfg.K8sCacheTTL)
	rateLimiter := NewRateLimiter(cfg.K8sRateLimit)

	return &Resolver{
		client:      client,
		cache:       cache,
		rateLimiter: rateLimiter,
		config:      cfg,
	}, nil
}

// ResolveIP resolves an IP address to Kubernetes service info
func (r *Resolver) ResolveIP(ctx context.Context, ip string) (*ServiceInfo, error) {
	// 1. Check cache first
	if cached := r.cache.Get(ip); cached != nil {
		slog.Debug("cache hit for IP",
			slog.String("ip", ip),
			slog.String("namespace", cached.Namespace),
			slog.String("service", cached.Service),
		)
		return cached, nil
	}

	// 2. Apply rate limiting
	if err := r.rateLimiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("rate limiter wait failed: %w", err)
	}

	// 3. Query Kubernetes API
	info, err := r.queryK8sAPI(ctx, ip)
	if err != nil {
		// Graceful fallback: return IP as-is
		slog.Debug("failed to resolve IP, falling back to raw IP",
			slog.String("ip", ip),
			slog.String("error", err.Error()),
		)
		fallback := &ServiceInfo{
			Service:   ip,
			Namespace: "",
			Pod:       "",
		}
		// Cache the fallback too to avoid repeated lookups
		r.cache.Set(ip, fallback)
		return fallback, nil
	}

	// 4. Cache result
	r.cache.Set(ip, info)

	slog.Debug("resolved IP",
		slog.String("ip", ip),
		slog.String("namespace", info.Namespace),
		slog.String("service", info.Service),
		slog.String("pod", info.Pod),
	)

	return info, nil
}

// queryK8sAPI queries the Kubernetes API for pod information by IP
func (r *Resolver) queryK8sAPI(ctx context.Context, ip string) (*ServiceInfo, error) {
	// Set timeout for K8s API call
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Strip IPv6-mapped IPv4 prefix (::ffff:) if present
	// ClickHouse returns IPs as ::ffff:10.0.1.100 but K8s expects 10.0.1.100
	cleanIP := ip
	if len(ip) > 7 && ip[:7] == "::ffff:" {
		cleanIP = ip[7:]
		slog.Debug("stripped IPv6-mapped prefix",
			slog.String("ip", ip),
			slog.String("clean_ip", cleanIP),
		)
	}

	// Strategy 1: Try to find by pod IP
	pods, err := r.client.Clientset().CoreV1().Pods("").List(queryCtx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("status.podIP=%s", cleanIP),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) > 0 {
		// Found by pod IP - use the first pod
		pod := pods.Items[0]
		return r.resolvePodToService(queryCtx, &pod)
	}

	// Strategy 2: Try to find by service IP (ClusterIP, LoadBalancer, External IP)
	slog.Debug("no pod found, trying service IPs", slog.String("ip", cleanIP))

	serviceInfo, err := r.findServiceByIP(queryCtx, cleanIP)
	if err == nil && serviceInfo != nil {
		slog.Debug("found service by IP",
			slog.String("namespace", serviceInfo.Namespace),
			slog.String("service", serviceInfo.Service),
		)
		return serviceInfo, nil
	}

	// No pod or service found
	return nil, fmt.Errorf("no pod or service found with IP %s", cleanIP)
}

// resolvePodToService resolves a pod to its owning service
func (r *Resolver) resolvePodToService(ctx context.Context, pod *corev1.Pod) (*ServiceInfo, error) {
	// Try to find the owning service
	serviceName := ""
	labels := pod.Labels

	if len(labels) > 0 {
		// Query services in the same namespace
		services, err := r.client.Clientset().CoreV1().Services(pod.Namespace).List(ctx, metav1.ListOptions{})
		if err == nil {
			// Find service that matches pod labels
			for _, svc := range services.Items {
				if matchesSelector(labels, svc.Spec.Selector) {
					serviceName = svc.Name
					break
				}
			}
		}
	}

	// If no service found, use pod name
	if serviceName == "" {
		serviceName = pod.Name
	}

	return &ServiceInfo{
		Service:   serviceName,
		Namespace: pod.Namespace,
		Pod:       pod.Name,
	}, nil
}

// findServiceByIP searches for a service with the given IP
// Checks ClusterIP, LoadBalancer IPs, and External IPs
func (r *Resolver) findServiceByIP(ctx context.Context, ip string) (*ServiceInfo, error) {
	// Get all services across all namespaces
	services, err := r.client.Clientset().CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	for _, svc := range services.Items {
		// Check ClusterIP
		if svc.Spec.ClusterIP == ip {
			slog.Debug("matched ClusterIP",
				slog.String("namespace", svc.Namespace),
				slog.String("service", svc.Name),
				slog.String("ip", ip),
			)
			return &ServiceInfo{
				Service:   svc.Name,
				Namespace: svc.Namespace,
				Pod:       "", // Service IP, not a specific pod
			}, nil
		}

		// Check LoadBalancer Ingress IPs
		for _, ingress := range svc.Status.LoadBalancer.Ingress {
			if ingress.IP == ip {
				slog.Debug("matched LoadBalancer IP",
					slog.String("namespace", svc.Namespace),
					slog.String("service", svc.Name),
					slog.String("ip", ip),
				)
				return &ServiceInfo{
					Service:   svc.Name,
					Namespace: svc.Namespace,
					Pod:       "", // LoadBalancer IP
				}, nil
			}
		}

		// Check ExternalIPs
		for _, externalIP := range svc.Spec.ExternalIPs {
			if externalIP == ip {
				slog.Debug("matched ExternalIP",
					slog.String("namespace", svc.Namespace),
					slog.String("service", svc.Name),
					slog.String("ip", ip),
				)
				return &ServiceInfo{
					Service:   svc.Name,
					Namespace: svc.Namespace,
					Pod:       "", // External IP
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("no service found with IP %s", ip)
}

// matchesSelector checks if pod labels match service selector
func matchesSelector(podLabels, svcSelector map[string]string) bool {
	if len(svcSelector) == 0 {
		return false
	}

	for key, value := range svcSelector {
		if podLabels[key] != value {
			return false
		}
	}

	return true
}

// Close closes the resolver (currently a no-op)
func (r *Resolver) Close() error {
	// Nothing to close currently
	return nil
}
