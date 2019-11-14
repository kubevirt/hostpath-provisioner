package tests

import (
	"testing"

	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSCExists(t *testing.T) {
	RegisterTestingT(t)
	tearDown, k8sClient := setupTestCase(t)
	defer tearDown(t)

	sc, err := k8sClient.StorageV1().StorageClasses().Get("hostpath-provisioner", metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())
	Expect(sc.Name).To(Equal("hostpath-provisioner"))
}
