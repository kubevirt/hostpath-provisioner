package tests

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	hostpathprovisioner "kubevirt.io/hostpath-provisioner-operator/pkg/client/clientset/versioned"

	"github.com/onsi/gomega"
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

// Common allocation units
const (
	KiB int64 = 1024
	MiB int64 = 1024 * KiB
	GiB int64 = 1024 * MiB
	TiB int64 = 1024 * GiB
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
	ns, err := k8sClient.CoreV1().Namespaces().Create(context.TODO(), createNamespace(), metav1.CreateOptions{})
	return func(t *testing.T) {
		t.Logf("Removing namespace: %s", ns.Name)
		err := k8sClient.CoreV1().Namespaces().Delete(context.TODO(), ns.Name, metav1.DeleteOptions{})
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
	return k8sClient.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
}

// getHPPClient returns a HPP rest client
func getHPPClient() (*hostpathprovisioner.Clientset, error) {
	cfg, err := clientcmd.BuildConfigFromFlags(*master, *kubeConfig)
	if err != nil {
		return nil, err
	}
	return hostpathprovisioner.NewForConfig(cfg)
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

// RunGoCLICommand executes gocli with given args
func RunGoCLICommand(cliPath string, args ...string) (string, error) {
	var outBuf, errBuf bytes.Buffer
	cmd := exec.Command(cliPath, args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	if err != nil {
		wd, _ := os.Getwd()
		fmt.Fprintf(os.Stderr, "Working dir: %s\n", wd)
		fmt.Fprintf(os.Stderr, "GoCLI standard output\n%s\n", outBuf.String())
		fmt.Fprintf(os.Stderr, "GoCLI error output\n%s\n", errBuf.String())
		return "", err
	}

	capture := false
	returnBuf := bytes.NewBuffer(nil)
	scanner := bufio.NewScanner(&outBuf)
	for scanner.Scan() {
		t := scanner.Text()
		if !capture {
			if strings.Contains(t, "Connected to tcp://192.168.66.") {
				capture = true
			}
			continue
		}
		_, err = returnBuf.Write([]byte(t))
		if err != nil {
			return "", err
		}
	}
	return returnBuf.String(), nil
}

// RunKubeCtlCommand executes gocli with given args
func RunKubeCtlCommand(args ...string) (string, error) {
	var outBuf, errBuf bytes.Buffer
	cmd := exec.Command("../cluster-up/kubectl.sh", args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err := cmd.Run()
	if err != nil {
		wd, _ := os.Getwd()
		fmt.Fprintf(os.Stderr, "Working dir: %s\n", wd)
		fmt.Fprintf(os.Stderr, "kubectl standard output\n%s\n", outBuf.String())
		fmt.Fprintf(os.Stderr, "kubectl error output\n%s\n", errBuf.String())
		return "", err
	}

	returnBuf := bytes.NewBuffer(nil)
	scanner := bufio.NewScanner(&outBuf)
	for scanner.Scan() {
		t := scanner.Text()
		_, err = returnBuf.Write([]byte(t))
		if err != nil {
			return "", err
		}
	}
	return returnBuf.String(), nil
}

// Round down the capacity to an easy to read value. Blatantly stolen from here: https://github.com/kubernetes-incubator/external-storage/blob/master/local-volume/provisioner/pkg/discovery/discovery.go#L339
func roundDownCapacityPretty(capacityBytes int64) int64 {

	easyToReadUnitsBytes := []int64{GiB, MiB}

	// Round down to the nearest easy to read unit
	// such that there are at least 10 units at that size.
	for _, easyToReadUnitBytes := range easyToReadUnitsBytes {
		// Round down the capacity to the nearest unit.
		size := capacityBytes / easyToReadUnitBytes
		if size >= 10 {
			return size * easyToReadUnitBytes
		}
	}
	return capacityBytes
}

// Assure reconcile events occur
func checkReconcileEventsOccur() {
	// These events are fired when the reconcile loop makes a change
	gomega.Eventually(func() string {
		out, err := RunKubeCtlCommand("describe", "hostpathprovisioner", "hostpath-provisioner")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		return out
	}, 90*time.Second, 1*time.Second).Should(gomega.ContainSubstring("UpdateResourceStart"))

	gomega.Eventually(func() string {
		out, err := RunKubeCtlCommand("describe", "hostpathprovisioner", "hostpath-provisioner")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		return out
	}, 90*time.Second, 1*time.Second).Should(gomega.ContainSubstring("UpdateResourceSuccess"))
}
