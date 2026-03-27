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
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	maxStoragePoolNameLength = 50
	maxPathLength            = 255
)

// SetupWebhookWithManager configures the webhook for the passed in manager
func (r *HostPathProvisioner) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).WithValidator(&HostPathProvisionerValidator{}).
		Complete()
}

type HostPathProvisionerValidator struct {
}

var _ webhook.CustomValidator = &HostPathProvisionerValidator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (v *HostPathProvisionerValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	hpp, ok := obj.(*HostPathProvisioner)
	if !ok {
		return nil, fmt.Errorf("obj is not a HostPathProvisioner")
	}
	return v.validatePathConfigAndStoragePools(hpp)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (v *HostPathProvisionerValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (warnings admission.Warnings, err error) {
	hpp, ok := newObj.(*HostPathProvisioner)
	if !ok {
		return nil, fmt.Errorf("newObj is not a HostPathProvisioner")
	}
	return v.validatePathConfigAndStoragePools(hpp)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (v *HostPathProvisionerValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (warnings admission.Warnings, err error) {
	return nil, nil
}

func (v *HostPathProvisionerValidator) validatePathConfigAndStoragePools(hpp *HostPathProvisioner) (admission.Warnings, error) {
	if hpp.Spec.PathConfig != nil && len(hpp.Spec.StoragePools) > 0 {
		return nil, fmt.Errorf("pathConfig and storage pools cannot be both set")
	} else if hpp.Spec.PathConfig == nil && len(hpp.Spec.StoragePools) == 0 {
		return nil, fmt.Errorf("either pathConfig or storage pools must be set")
	}
	if hpp.Spec.PathConfig != nil && len(hpp.Spec.PathConfig.Path) == 0 {
		return nil, fmt.Errorf("pathconfig path must be set")
	}
	usedPaths := make(map[string]int, 0)
	usedNames := make(map[string]int, 0)
	for i, source := range hpp.Spec.StoragePools {
		if err := validateStoragePool(source); err != nil {
			return nil, err
		}
		if index, ok := usedPaths[source.Path]; !ok {
			usedPaths[source.Path] = i
		} else {
			return nil, fmt.Errorf("spec.storagePools[%d].path is the same as spec.storagePools[%d].path, cannot have duplicate paths", i, index)
		}
		if index, ok := usedNames[source.Name]; !ok {
			usedNames[source.Name] = i
		} else {
			return nil, fmt.Errorf("spec.storagePools[%d].name is the same as spec.storagePools[%d].name, cannot have duplicate names", i, index)
		}
	}
	return nil, nil
}

func validateStoragePool(storagePool StoragePool) error {
	if storagePool.Name == "" {
		return fmt.Errorf("storagePool.name cannot be blank")
	}
	if len(storagePool.Name) > maxStoragePoolNameLength {
		return fmt.Errorf("storagePool.name cannot have a length greater than 50")
	}
	if storagePool.Path == "" {
		return fmt.Errorf("storagePool.path cannot be blank")
	}
	if len(storagePool.Path) > maxPathLength {
		return fmt.Errorf("storagePool.path cannot have a length greater than 255")
	}
	return nil
}
