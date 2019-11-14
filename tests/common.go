package tests

import (
	"flag"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	kubectlPath = flag.String("kubectl-path", "kubectl", "The path to the kubectl binary")
	kubeConfig  = flag.String("kubeconfig", "/var/run/kubernetes/admin.kubeconfig", "The absolute path to the kubeconfig file")
	master      = flag.String("master", "", "master url:port")
)

func setupTestCase(t *testing.T) (func(*testing.T), *kubernetes.Clientset) {
	k8sClient, err := getKubeClient()
	if err != nil {
		t.Errorf("ERROR, unable to create K8SClient: %v", err)
	}
	return func(t *testing.T) {
		// TODO, any k8s cleanup.
	}, k8sClient
}

func setupTestCaseNs(t *testing.T) (func(*testing.T), *corev1.Namespace, *kubernetes.Clientset) {
	k8sClient, err := getKubeClient()
	if err != nil {
		t.Errorf("ERROR, unable to create K8SClient: %v", err)
	}
	ns, err := k8sClient.CoreV1().Namespaces().Create(createNamespace())
	return func(t *testing.T) {
		t.Logf("Removing namespace: %s", ns.Name)
		err := k8sClient.CoreV1().Namespaces().Delete(ns.Name, &metav1.DeleteOptions{})
		if err != nil {
			t.Errorf("ERROR, unable to remove namespace: %s, %v", ns.Name, err)
		}
	}, ns, k8sClient
}

func createNamespace() *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "hpp-test",
			Namespace:    "",
			Labels:       map[string]string{},
		},
		Status: corev1.NamespaceStatus{},
	}
}

func getAllNodes(k8sClient *kubernetes.Clientset) (*corev1.NodeList, error) {
	return k8sClient.CoreV1().Nodes().List(metav1.ListOptions{})
}

// getKubeClient returns a Kubernetes rest client
func getKubeClient() (*kubernetes.Clientset, error) {
	cmd, err := clientcmd.BuildConfigFromFlags(*master, *kubeConfig)
	if err != nil {
		return nil, err
	}
	return getKubeClientFromRESTConfig(cmd)
}

// getKubeClientFromRESTConfig provides a function to get a K8s client using hte REST config
func getKubeClientFromRESTConfig(config *rest.Config) (*kubernetes.Clientset, error) {
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	return kubernetes.NewForConfig(config)
}
