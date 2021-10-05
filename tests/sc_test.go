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
	"testing"

	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSCExists(t *testing.T) {
	RegisterTestingT(t)
	tearDown, k8sClient := setupTestCase(t)
	defer tearDown(t)

	t.Run("legacy provisioner", func(t *testing.T) {
		sc, err := k8sClient.StorageV1().StorageClasses().Get(context.TODO(), legacyStorageClassName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(sc.Name).To(Equal(legacyStorageClassName))
	})
	t.Run("legacy provisioner immediate", func(t *testing.T) {
		sc, err := k8sClient.StorageV1().StorageClasses().Get(context.TODO(), legacyStorageClassNameImmediate, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(sc.Name).To(Equal(legacyStorageClassNameImmediate))
	})
	t.Run("csi driver", func(t *testing.T) {
		sc, err := k8sClient.StorageV1().StorageClasses().Get(context.TODO(), csiStorageClassName, metav1.GetOptions{})
		Expect(err).ToNot(HaveOccurred())
		Expect(sc.Name).To(Equal(csiStorageClassName))
	})
}
