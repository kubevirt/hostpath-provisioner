/*
Copyright 2019 The hostpath provisioner operator Authors.

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

package v1beta1

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// SetupWebhookWithManager configures the webhook for the passed in manager
func (r *HostPathProvisioner) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

var _ webhook.Validator = &HostPathProvisioner{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *HostPathProvisioner) ValidateCreate() error {
	return r.validatePathConfigAndStoragePools()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *HostPathProvisioner) ValidateUpdate(old runtime.Object) error {
	return r.validatePathConfigAndStoragePools()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *HostPathProvisioner) ValidateDelete() error {
	return nil
}

func (r *HostPathProvisioner) validatePathConfigAndStoragePools() error {
	if r.Spec.PathConfig != nil && len(r.Spec.StoragePools) > 0 {
		return fmt.Errorf("pathConfig and storage pools cannot be both set")
	} else if r.Spec.PathConfig == nil && len(r.Spec.StoragePools) == 0 {
		return fmt.Errorf("either pathConfig or storage pools must be set")
	}
	if r.Spec.PathConfig != nil && len(r.Spec.PathConfig.Path) == 0 {
		return fmt.Errorf("pathconfig path must be set")
	}
	if len(r.Spec.StoragePools) > 1 {
		return fmt.Errorf("currently only 1 storage pool is supported")
	}
	for _, source := range r.Spec.StoragePools {
		if err := validateStoragePool(source); err != nil {
			return err
		}
	}
	return nil
}

func validateStoragePool(storagePool StoragePool) error {
	if storagePool.Name == "" {
		return fmt.Errorf("storagePool.kind cannot be blank")
	}
	if storagePool.Path == "" {
		return fmt.Errorf("storagePool.path cannot be blank")
	}
	return nil
}
