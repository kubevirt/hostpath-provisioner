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
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	extclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	monitoringNs = "monitoring"
	prometheusCRDName = "prometheuses.monitoring.coreos.com"
	prometheusSaName = "prometheus-k8s"
	prometheusSaSecretPrefix = "prometheus-k8s-token"
	operatorUpQueryName = "kubevirt_hpp_operator_up_total"
	hppCRReadyQueryName = "kubevirt_hpp_cr_ready"
	promRuleOperatorUp = "1"
	promRuleOperatorDown = "0"
	promRuleCRReady = "1"
	operatorDeploymentName = "hostpath-provisioner-operator"
)

type promQueryResult struct {
	Status string `json:"status"`
	Data promData  `json:"data"`
}

type promData struct {
	ResultType string `json:"resultType"`
	Result []promResult `json:"result"`
}

type promResult struct {
	Metric promMetric `json:"metric"`
	Value []interface{} `json:"value"`
}

type promMetric struct {
	Name string `json:"__name__"`
}

func TestPrometheusMetrics(t *testing.T) {
	RegisterTestingT(t)
	tearDown, _, k8sClient := setupTestCaseNs(t)
	defer tearDown(t)
	defer func() {
		err := scaleOperatorUp(k8sClient)
		Expect(err).ToNot(HaveOccurred(), "Unable to scale operator back up.")
	} ()
	extClient, err := getExtClient()
	Expect(err).ToNot(HaveOccurred())

	if !IsPrometheusAvailable(extClient) {
		t.Skip("Skipping because prometheus is not available")
	}
	token, err := getPrometheusSaToken(k8sClient)
	Expect(err).ToNot(HaveOccurred())
	Expect(token).ToNot(BeEmpty())
	t.Run("Operator Up", func(t *testing.T) {
		testPrometheusRule(token, operatorUpQueryName, promRuleOperatorUp, t)
	})
	t.Run("HPP CR ready", func(t *testing.T){
		testPrometheusRule(token, hppCRReadyQueryName, promRuleCRReady, t)
	})
	err = scaleOperatorDown(k8sClient)
	Expect(err).ToNot(HaveOccurred())
	t.Run("Operator Down", func(t *testing.T) {
		testPrometheusRule(token, operatorUpQueryName, promRuleOperatorDown, t)
	})
}

func testPrometheusRule (token, promQuery, value string, t *testing.T) {
	prometheusURL := fmt.Sprintf("%s/api/v1/query?query=%s", getPrometheusBaseURL(), promQuery)
	url, err := url.Parse(prometheusURL)
	Expect(err).ToNot(HaveOccurred())
	var result promQueryResult
	Eventually(func() bool {
		bodyBytes := makePrometheusHTTPRequest(url, token)
		t.Logf("body: %s", bodyBytes)
		err := json.Unmarshal(bodyBytes, &result)
		Expect(err).ToNot(HaveOccurred())
		return len(result.Data.Result) > 0 &&
			len(result.Data.Result[0].Value) > 1 &&
			result.Data.Result[0].Value[1] == value &&
			result.Data.Result[0].Metric.Name == url.Query().Get("query")
	}, 2*time.Minute, 1*time.Second).Should(BeTrue())
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
	baseUrl := fmt.Sprintf("http://localhost:%s", port)
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
	secret, err := k8sClient.CoreV1().Secrets(monitoringNs).Get(context.TODO(), secretName, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	if _, ok := secret.Data["token"]; !ok {
		return "", fmt.Errorf("No token data in secret")
	}
	return string(secret.Data["token"]), nil
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
		deployment, _ := k8sClient.AppsV1().Deployments(namespace).Get(context.TODO(), operatorDeploymentName, metav1.GetOptions{})
		return deployment.Status.AvailableReplicas
	}, 1*time.Minute, time.Second).Should(Equal(*count))

	return nil
}