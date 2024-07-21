/*
Copyright 2021 The hostpath provisioner Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package tests

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	. "github.com/onsi/gomega"

	hostpathprovisionerv1 "kubevirt.io/hostpath-provisioner-operator/pkg/apis/hostpathprovisioner/v1beta1"
	hostpathprovisioner "kubevirt.io/hostpath-provisioner-operator/pkg/client/clientset/versioned"

	authenticationv1 "k8s.io/api/authentication/v1"
	extclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	monitoringNs             = "monitoring"
	prometheusCRDName        = "prometheuses.monitoring.coreos.com"
	prometheusSaName         = "prometheus-k8s"
	prometheusSaSecretPrefix = "prometheus-k8s-token"
	operatorUpQueryName      = "kubevirt_hpp_operator_up"
	hppCRReadyQueryName      = "kubevirt_hpp_cr_ready"
	hppPoolSharedQueryName   = "kubevirt_hpp_pool_path_shared_with_os"
	promRuleOperatorUp       = "1"
	promRuleOperatorDown     = "0"
	promRuleCRReady          = "1"
	promRuleCRNotReady       = "0"
	operatorDeploymentName   = "hostpath-provisioner-operator"
)

type promQueryResult struct {
	Status string   `json:"status"`
	Data   promData `json:"data"`
}

type promData struct {
	ResultType string       `json:"resultType"`
	Result     []promResult `json:"result"`
	Alerts     []promAlert  `json:"alerts"`
}

type promResult struct {
	Metric promMetric    `json:"metric"`
	Value  []interface{} `json:"value"`
}

type promAlert struct {
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	State       string            `json:"state"`
	Value       string            `json:"value"`
}

type promMetric struct {
	Name string `json:"__name__"`
}

func TestPrometheusMetrics(t *testing.T) {
	k8sClient, _, token := prometheusTestSetup(t)

	t.Run("Operator Up", func(t *testing.T) {
		testPrometheusRule(token, operatorUpQueryName, promRuleOperatorUp)
	})
	t.Run("HPP CR ready", func(t *testing.T) {
		testPrometheusRule(token, hppCRReadyQueryName, promRuleCRReady)
	})
	t.Run("HPP pool sharing path with OS", func(t *testing.T) {
		promRulePoolShared := "0"
		backingStorage := os.Getenv("KUBEVIRT_STORAGE")
		hppCrType := os.Getenv("HPP_CR_TYPE")
		// Our only CI setup that avoids sharing path with OS
		// is a backing rook-ceph-block PVC of the HPP storage pool
		shared := backingStorage != "rook-ceph-default" || hppCrType != "storagepool-pvc-template"
		if shared {
			promRulePoolShared = "1"
		}
		testPrometheusRule(token, hppPoolSharedQueryName, promRulePoolShared)
	})
	err := scaleOperatorDown(k8sClient)
	Expect(err).ToNot(HaveOccurred())
	t.Run("Operator Down", func(t *testing.T) {
		testPrometheusRule(token, operatorUpQueryName, promRuleOperatorDown)
	})
	t.Run("HPP alert rules", func(t *testing.T) {
		testAlertRules(k8sClient)
	})
}

func TestPrometheusAlerts(t *testing.T) {
	k8sClient, hppClient, token := prometheusTestSetup(t)

	getHPP := func() *hostpathprovisionerv1.HostPathProvisioner {
		hpp, err := hppClient.HostpathprovisionerV1beta1().HostPathProvisioners().Get(context.Background(), "hostpath-provisioner", metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		return hpp
	}

	updateHPP := func(hpp *hostpathprovisionerv1.HostPathProvisioner) {
		_, err := hppClient.HostpathprovisionerV1beta1().HostPathProvisioners().Update(context.Background(), hpp, metav1.UpdateOptions{})
		Expect(err).ToNot(HaveOccurred())
	}

	hpp := getHPP()
	oldSpec := hpp.Spec.DeepCopy()

	t.Run("HPPOperatorDown", func(t *testing.T) {
		err := scaleOperatorDown(k8sClient)
		Expect(err).ToNot(HaveOccurred())

		testPrometheusAlert("HPPOperatorDown", token, t)

		err = scaleOperatorUp(k8sClient)
		Expect(err).ToNot(HaveOccurred())
	})

	t.Run("HPPNotReady", func(t *testing.T) {
		hpp.Spec.Workload.NodeSelector = map[string]string{"non-existing": "label"}
		updateHPP(hpp)

		testPrometheusRule(token, hppCRReadyQueryName, promRuleCRNotReady)

		testPrometheusAlert("HPPNotReady", token, t)

		hpp := getHPP()
		hpp.Spec = *oldSpec
		updateHPP(hpp)

		testPrometheusRule(token, hppCRReadyQueryName, promRuleCRReady)
	})

	_ = scaleOperatorUp(k8sClient)
	hpp = getHPP()
	hpp.Spec = *oldSpec
	updateHPP(hpp)
}

func prometheusTestSetup(t *testing.T) (*kubernetes.Clientset, *hostpathprovisioner.Clientset, string) {
	RegisterTestingT(t)
	tearDown, _, k8sClient := setupTestCaseNs(t)
	defer tearDown(t)
	defer func() {
		err := scaleOperatorUp(k8sClient)
		Expect(err).ToNot(HaveOccurred(), "Unable to scale operator back up.")
	}()
	extClient, err := getExtClient()
	Expect(err).ToNot(HaveOccurred())

	if !IsPrometheusAvailable(extClient) {
		t.Skip("Skipping because prometheus is not available")
	}
	token, err := getPrometheusSaToken(k8sClient)
	Expect(err).ToNot(HaveOccurred())
	Expect(token).ToNot(BeEmpty())

	hppClient, err := getHPPClient()
	Expect(err).ToNot(HaveOccurred())

	return k8sClient, hppClient, token
}

func testPrometheusRule(token, promQuery, value string) {
	prometheusURL := fmt.Sprintf("%s/api/v1/query?query=%s", getPrometheusBaseURL(), promQuery)
	url, err := url.Parse(prometheusURL)
	Expect(err).ToNot(HaveOccurred())
	var result promQueryResult
	Eventually(func() bool {
		bodyBytes := makePrometheusHTTPRequest(url, token)
		err := json.Unmarshal(bodyBytes, &result)
		Expect(err).ToNot(HaveOccurred())
		return len(result.Data.Result) > 0 &&
			len(result.Data.Result[0].Value) > 1 &&
			result.Data.Result[0].Value[1] == value &&
			result.Data.Result[0].Metric.Name == url.Query().Get("query")
	}, 2*time.Minute, 1*time.Second).Should(BeTrue())
}

func testAlertRules(k8sClient *kubernetes.Clientset) {
	var promRule monitoringv1.PrometheusRule
	err := k8sClient.RESTClient().Get().
		Resource("prometheusrules").
		Name("prometheus-hpp-rules").
		Namespace(namespace).
		AbsPath("/apis", monitoringv1.SchemeGroupVersion.Group, monitoringv1.SchemeGroupVersion.Version).
		Timeout(10 * time.Second).
		Do(context.TODO()).Into(&promRule)
	Expect(err).ToNot(HaveOccurred())
	Expect(promRule.Spec.Groups).ToNot(BeEmpty())
	for _, group := range promRule.Spec.Groups {
		if group.Name == "hpp.rules" {
			Expect(group.Rules).ToNot(BeEmpty())
			for _, rule := range group.Rules {
				if rule.Alert != "" {
					Expect(rule.Annotations).ToNot(BeNil())
					Expect(rule.Labels).ToNot(BeNil())
					checkRequiredAnnotations(rule)
					checkRequiredLabels(rule)
				}
			}
		}
	}
}

// IsPrometheusAvailable decides whether or not we will run prometheus alert/metric tests
func IsPrometheusAvailable(client *extclientset.Clientset) bool {
	_, err := client.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), prometheusCRDName, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		return false
	}
	return true
}

func getPrometheusBaseURL() string {
	var port string
	var err error

	Eventually(func() error {
		port, err = RunGoCLICommand("ports", "prometheus")
		return err
	}, 10*time.Second, time.Second).Should(BeNil())
	Expect(port).ToNot(BeEmpty())
	port = strings.TrimSpace(port)
	Expect(port).ToNot(BeEmpty())
	baseUrl := fmt.Sprintf("http://127.0.0.1:%s", port)
	return baseUrl
}

func makePrometheusHTTPRequest(url *url.URL, token string) []byte {
	var resp *http.Response

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	req, err := http.NewRequest("GET", url.String(), nil)
	Expect(err).ToNot(HaveOccurred())
	req.Header.Add("Authorization", "Bearer "+token)
	resp, err = client.Do(req)
	Expect(err).ToNot(HaveOccurred())
	defer resp.Body.Close()
	Expect(resp.StatusCode).To(Equal(http.StatusOK), url.String())
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	Expect(err).ToNot(HaveOccurred())
	return bodyBytes
}

func getPrometheusSaToken(k8sClient *kubernetes.Clientset) (string, error) {
	var secretName string
	sa, err := k8sClient.CoreV1().ServiceAccounts(monitoringNs).Get(context.TODO(), prometheusSaName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	for _, secret := range sa.Secrets {
		if strings.HasPrefix(secret.Name, prometheusSaSecretPrefix) {
			secretName = secret.Name
		}
	}
	if secretName == "" {
		// Since 1.24 SAs don't have tokens automatically generated, we determined the SA has no secret, so we
		// need to generate one.
		token, err := k8sClient.CoreV1().ServiceAccounts(monitoringNs).CreateToken(
			context.TODO(),
			prometheusSaName,
			&authenticationv1.TokenRequest{
				Spec: authenticationv1.TokenRequestSpec{},
			},
			metav1.CreateOptions{},
		)
		Expect(err).ToNot(HaveOccurred())
		return token.Name, nil
	} else {
		secret, err := k8sClient.CoreV1().Secrets(monitoringNs).Get(context.TODO(), secretName, metav1.GetOptions{})
		if err != nil {
			return "", err
		}
		if _, ok := secret.Data["token"]; !ok {
			return "", fmt.Errorf("No token data in secret")
		}
		return string(secret.Data["token"]), nil
	}
}

func checkRequiredAnnotations(rule monitoringv1.Rule) {
	Expect(rule.Annotations).To(HaveKeyWithValue("summary", Not(BeEmpty())),
		"%s summary is missing or empty", rule.Alert)
	Expect(rule.Annotations).To(HaveKey("runbook_url"),
		"%s runbook_url is missing", rule.Alert)
	Expect(rule.Annotations).To(HaveKeyWithValue("runbook_url", ContainSubstring(rule.Alert)),
		"%s runbook_url doesn't include alert name", rule.Alert)
	resp, err := http.Head(rule.Annotations["runbook_url"])
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), fmt.Sprintf("%s runbook is not available", rule.Alert))
	ExpectWithOffset(1, resp.StatusCode).Should(Equal(http.StatusOK), fmt.Sprintf("%s runbook is not available", rule.Alert))
}

func checkRequiredLabels(rule monitoringv1.Rule) {
	Expect(rule.Labels).To(HaveKeyWithValue("severity", BeElementOf("info", "warning", "critical")),
		"%s severity label is missing or not valid", rule.Alert)
	Expect(rule.Labels).To(HaveKeyWithValue("kubernetes_operator_part_of", "kubevirt"),
		"%s kubernetes_operator_part_of label is missing or not valid", rule.Alert)
	Expect(rule.Labels).To(HaveKeyWithValue("kubernetes_operator_component", "hostpath-provisioner-operator"),
		"%s kubernetes_operator_component label is missing or not valid", rule.Alert)
	Expect(rule.Labels).To(HaveKeyWithValue("operator_health_impact", BeElementOf("none", "warning", "critical")),
		"%s operator_health_impact label is missing or not valid", rule.Alert)
}

func scaleOperatorDown(k8sClient *kubernetes.Clientset) error {
	zero := int32(0)
	return scaleOperator(k8sClient, &zero)
}

func scaleOperatorUp(k8sClient *kubernetes.Clientset) error {
	one := int32(1)
	return scaleOperator(k8sClient, &one)
}

func scaleOperator(k8sClient *kubernetes.Clientset, count *int32) error {
	deployment, err := k8sClient.AppsV1().Deployments(namespace).Get(context.TODO(), operatorDeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	deployment.Spec.Replicas = count
	deployment, err = k8sClient.AppsV1().Deployments(namespace).Update(context.TODO(), deployment, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	Eventually(func() int32 {
		deployment, _ = k8sClient.AppsV1().Deployments(namespace).Get(context.TODO(), operatorDeploymentName, metav1.GetOptions{})
		return deployment.Status.AvailableReplicas
	}, 1*time.Minute, time.Second).Should(Equal(*count))

	return nil
}

func testPrometheusAlert(alertName string, token string, t *testing.T) {
	prometheusURL := fmt.Sprintf("%s/api/v1/alerts", getPrometheusBaseURL())
	promUrl, err := url.Parse(prometheusURL)
	Expect(err).ToNot(HaveOccurred())
	var result promQueryResult

	Eventually(func() error {
		bodyBytes := makePrometheusHTTPRequest(promUrl, token)
		t.Logf("body: %s", bodyBytes)
		err = json.Unmarshal(bodyBytes, &result)
		if err != nil {
			return err
		}

		for _, r := range result.Data.Alerts {
			t.Logf("alertname: %s, state: %s", r.Labels["alertname"], r.State)

			if r.Labels["alertname"] == alertName && (r.State == "firing" || r.State == "pending") {
				return nil
			}
		}

		return errors.New(fmt.Sprintf("alert %s not found or not firing", alertName))
	}, 10*time.Minute, 1*time.Minute).Should(Not(HaveOccurred()))
}
