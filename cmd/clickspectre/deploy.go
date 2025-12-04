package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Embedded Kubernetes manifests templates
var (
	deploymentName = "clickspectre-report"
	serviceName    = "clickspectre-report"
	configMapName  = "clickspectre-report-data"
)

// NewDeployCmd creates the deploy command
func NewDeployCmd() *cobra.Command {
	var kubeconfig string
	var namespace string
	var port int
	var openBrowser bool
	var ingressHost string
	var reportDir string

	cmd := &cobra.Command{
		Use:   "deploy [report-directory]",
		Short: "Deploy report to Kubernetes",
		Long: `Deploy ClickSpectre report to Kubernetes cluster.

This command will:
  1. Create namespace (if it doesn't exist)
  2. Create ConfigMap from report files
  3. Deploy nginx pod to serve the report
  4. Create Service
  5. Optionally set up port-forwarding
  6. Optionally create Ingress for external access`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				reportDir = args[0]
			}
			return runDeploy(kubeconfig, namespace, reportDir, port, openBrowser, ingressHost)
		},
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "Path to kubeconfig (default: ~/.kube/config)")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "default", "Kubernetes namespace")
	cmd.Flags().IntVarP(&port, "port", "p", 8080, "Local port for port-forward")
	cmd.Flags().BoolVar(&openBrowser, "open", true, "Automatically open browser")
	cmd.Flags().StringVar(&ingressHost, "ingress-host", "", "Host for Ingress (e.g., clickspectre.example.com)")
	cmd.Flags().StringVar(&reportDir, "report", "./report", "Report directory to deploy")

	return cmd
}

// runDeploy executes the Kubernetes deployment
func runDeploy(kubeconfigPath, namespace, reportDir string, localPort int, openBrowser bool, ingressHost string) error {
	ctx := context.Background()

	// Validate report directory
	if _, err := os.Stat(reportDir); os.IsNotExist(err) {
		return fmt.Errorf("report directory not found: %s\nRun 'clickspectre analyze' first", reportDir)
	}
	reportJSONPath := filepath.Join(reportDir, "report.json")
	if _, err := os.Stat(reportJSONPath); os.IsNotExist(err) {
		return fmt.Errorf("report.json not found in %s", reportDir)
	}

	fmt.Println("🚀 ClickSpectre Kubernetes Deployment")
	fmt.Println("=====================================")
	fmt.Printf("📂 Report: %s\n", reportDir)
	fmt.Printf("☸️  Namespace: %s\n", namespace)
	fmt.Printf("🔌 Local port: %d\n", localPort)
	fmt.Println()

	// Load kubeconfig
	if kubeconfigPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("failed to get home directory: %w", err)
		}
		kubeconfigPath = filepath.Join(home, ".kube", "config")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return fmt.Errorf("failed to load kubeconfig: %w\nMake sure kubectl is configured", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	// 1. Create namespace if it doesn't exist
	fmt.Printf("📦 Ensuring namespace '%s' exists...\n", namespace)
	if err := createNamespaceIfNotExists(ctx, clientset, namespace); err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	// 2. Create ConfigMap from report files
	fmt.Println("📤 Creating ConfigMap from report files...")
	if err := createConfigMapFromDirectory(ctx, clientset, namespace, reportDir); err != nil {
		return fmt.Errorf("failed to create ConfigMap: %w", err)
	}

	// 3. Create Deployment
	fmt.Println("🚢 Deploying report server...")
	if err := createDeployment(ctx, clientset, namespace); err != nil {
		return fmt.Errorf("failed to create deployment: %w", err)
	}

	// 4. Create Service
	fmt.Println("🌐 Creating Service...")
	if err := createService(ctx, clientset, namespace); err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	// 5. Create Ingress (if host specified)
	if ingressHost != "" {
		fmt.Printf("🔗 Creating Ingress for %s...\n", ingressHost)
		if err := createIngress(ctx, clientset, namespace, ingressHost); err != nil {
			log.Printf("Warning: Failed to create Ingress: %v", err)
		}
	}

	// 6. Wait for deployment to be ready
	fmt.Println("⏳ Waiting for deployment to be ready...")
	if err := waitForDeployment(ctx, clientset, namespace, 60*time.Second); err != nil {
		return fmt.Errorf("deployment failed to become ready: %w", err)
	}

	fmt.Println()
	fmt.Println("✅ Deployment complete!")
	fmt.Println()

	// 7. Set up port-forward
	fmt.Printf("🔌 Setting up port-forward to localhost:%d...\n", localPort)
	fmt.Printf("   (Press Ctrl+C to stop)\n")
	fmt.Println()

	if ingressHost != "" {
		fmt.Printf("🌍 External access: http://%s\n", ingressHost)
		fmt.Printf("   (Note: DNS and Ingress controller must be configured)\n")
		fmt.Println()
	}

	// Start port-forward
	stopCh := make(chan struct{}, 1)
	readyCh := make(chan struct{})

	go func() {
		if err := portForward(config, namespace, localPort, stopCh, readyCh); err != nil {
			log.Printf("Port-forward error: %v", err)
		}
	}()

	// Wait for port-forward to be ready
	select {
	case <-readyCh:
		fmt.Printf("✅ Port-forward ready at http://localhost:%d\n", localPort)
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout waiting for port-forward to be ready")
	}

	// Open browser
	if openBrowser {
		time.Sleep(1 * time.Second)
		url := fmt.Sprintf("http://localhost:%d", localPort)
		fmt.Printf("🌐 Opening browser: %s\n", url)
		if err := openURL(url); err != nil {
			log.Printf("Failed to open browser: %v", err)
		}
	}

	// Wait for interrupt
	<-stopCh

	return nil
}

// createNamespaceIfNotExists creates a namespace if it doesn't already exist
func createNamespaceIfNotExists(ctx context.Context, clientset *kubernetes.Clientset, namespace string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	_, err := clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	if errors.IsAlreadyExists(err) {
		fmt.Printf("   Namespace '%s' already exists\n", namespace)
	} else {
		fmt.Printf("   ✓ Created namespace '%s'\n", namespace)
	}

	return nil
}

// createConfigMapFromDirectory creates a ConfigMap from all files in a directory
func createConfigMapFromDirectory(ctx context.Context, clientset *kubernetes.Clientset, namespace, dir string) error {
	data := make(map[string]string)

	// Read all files in directory
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			// Handle subdirectories (like libs/)
			subdir := filepath.Join(dir, entry.Name())
			subentries, err := os.ReadDir(subdir)
			if err != nil {
				continue
			}
			for _, subentry := range subentries {
				if !subentry.IsDir() {
					path := filepath.Join(subdir, subentry.Name())
					content, err := os.ReadFile(path)
					if err != nil {
						log.Printf("Warning: Failed to read %s: %v", path, err)
						continue
					}
					key := filepath.Join(entry.Name(), subentry.Name())
					data[key] = string(content)
				}
			}
		} else {
			path := filepath.Join(dir, entry.Name())
			content, err := os.ReadFile(path)
			if err != nil {
				log.Printf("Warning: Failed to read %s: %v", path, err)
				continue
			}
			data[entry.Name()] = string(content)
		}
	}

	// Delete existing ConfigMap
	err = clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, configMapName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	// Create new ConfigMap
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
		},
		Data: data,
	}

	_, err = clientset.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	fmt.Printf("   ✓ Created ConfigMap with %d files\n", len(data))
	return nil
}

// createDeployment creates the nginx deployment
func createDeployment(ctx context.Context, clientset *kubernetes.Clientset, namespace string) error {
	replicas := int32(1)
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
			Labels: map[string]string{
				"app": deploymentName,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": deploymentName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": deploymentName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "nginx",
							Image: "nginx:alpine",
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 80,
									Name:          "http",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "report-data",
									MountPath: "/usr/share/nginx/html",
									ReadOnly:  true,
								},
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: mustParseQuantity("64Mi"),
									corev1.ResourceCPU:    mustParseQuantity("100m"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceMemory: mustParseQuantity("128Mi"),
									corev1.ResourceCPU:    mustParseQuantity("200m"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "report-data",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: configMapName,
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Delete existing deployment if it exists
	err := clientset.AppsV1().Deployments(namespace).Delete(ctx, deploymentName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	// Create deployment
	_, err = clientset.AppsV1().Deployments(namespace).Create(ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	fmt.Printf("   ✓ Created deployment '%s'\n", deploymentName)
	return nil
}

// createService creates the ClusterIP service
func createService(ctx context.Context, clientset *kubernetes.Clientset, namespace string) error {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
			Labels: map[string]string{
				"app": deploymentName,
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Port:       80,
					TargetPort: intstr.FromInt(80),
					Protocol:   corev1.ProtocolTCP,
					Name:       "http",
				},
			},
			Selector: map[string]string{
				"app": deploymentName,
			},
		},
	}

	// Delete existing service if it exists
	err := clientset.CoreV1().Services(namespace).Delete(ctx, serviceName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	// Create service
	_, err = clientset.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	fmt.Printf("   ✓ Created service '%s'\n", serviceName)
	return nil
}

// createIngress creates an Ingress resource for external access
func createIngress(ctx context.Context, clientset *kubernetes.Clientset, namespace, host string) error {
	pathType := networkingv1.PathTypePrefix
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: namespace,
			Annotations: map[string]string{
				"nginx.ingress.kubernetes.io/rewrite-target": "/",
			},
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: serviceName,
											Port: networkingv1.ServiceBackendPort{
												Number: 80,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Delete existing ingress if it exists
	err := clientset.NetworkingV1().Ingresses(namespace).Delete(ctx, deploymentName, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	// Create ingress
	_, err = clientset.NetworkingV1().Ingresses(namespace).Create(ctx, ingress, metav1.CreateOptions{})
	if err != nil {
		return err
	}

	fmt.Printf("   ✓ Created ingress for host '%s'\n", host)
	return nil
}

// waitForDeployment waits for the deployment to be ready
func waitForDeployment(ctx context.Context, clientset *kubernetes.Clientset, namespace string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		deployment, err := clientset.AppsV1().Deployments(namespace).Get(ctx, deploymentName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if deployment.Status.ReadyReplicas > 0 {
			fmt.Printf("   ✓ Deployment is ready (%d/%d replicas)\n", deployment.Status.ReadyReplicas, *deployment.Spec.Replicas)
			return nil
		}

		time.Sleep(2 * time.Second)
	}

	return fmt.Errorf("timeout waiting for deployment to be ready")
}

// portForward sets up port forwarding to the service
func portForward(config *rest.Config, namespace string, localPort int, stopCh, readyCh chan struct{}) error {
	// This is a simplified version - in production you'd use the full port-forward implementation
	// For now, we'll use kubectl port-forward as a subprocess
	cmd := exec.Command("kubectl", "port-forward",
		"-n", namespace,
		fmt.Sprintf("svc/%s", serviceName),
		fmt.Sprintf("%d:80", localPort),
	)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return err
	}

	// Signal ready after a short delay
	time.Sleep(2 * time.Second)
	close(readyCh)

	// Wait for stop signal or command to finish
	go func() {
		cmd.Wait()
		close(stopCh)
	}()

	<-stopCh
	return cmd.Process.Kill()
}

// openURL opens a URL in the default browser
func openURL(url string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return fmt.Errorf("unsupported platform")
	}

	return cmd.Start()
}

// Helper functions
func mustParseQuantity(s string) resource.Quantity {
	q, err := resource.ParseQuantity(s)
	if err != nil {
		panic(err)
	}
	return q
}
