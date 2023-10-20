/*
Copyright 2020 The hostpath provisioner operator Authors.

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

// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1beta1

import (
	v1 "k8s.io/api/core/v1"
)

// NodePlacementApplyConfiguration represents an declarative configuration of the NodePlacement type for use
// with apply.
type NodePlacementApplyConfiguration struct {
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	Affinity     *v1.Affinity      `json:"affinity,omitempty"`
	Tolerations  []v1.Toleration   `json:"tolerations,omitempty"`
}

// NodePlacementApplyConfiguration constructs an declarative configuration of the NodePlacement type for use with
// apply.
func NodePlacement() *NodePlacementApplyConfiguration {
	return &NodePlacementApplyConfiguration{}
}

// WithNodeSelector puts the entries into the NodeSelector field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, the entries provided by each call will be put on the NodeSelector field,
// overwriting an existing map entries in NodeSelector field with the same key.
func (b *NodePlacementApplyConfiguration) WithNodeSelector(entries map[string]string) *NodePlacementApplyConfiguration {
	if b.NodeSelector == nil && len(entries) > 0 {
		b.NodeSelector = make(map[string]string, len(entries))
	}
	for k, v := range entries {
		b.NodeSelector[k] = v
	}
	return b
}

// WithAffinity sets the Affinity field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Affinity field is set to the value of the last call.
func (b *NodePlacementApplyConfiguration) WithAffinity(value v1.Affinity) *NodePlacementApplyConfiguration {
	b.Affinity = &value
	return b
}

// WithTolerations adds the given value to the Tolerations field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the Tolerations field.
func (b *NodePlacementApplyConfiguration) WithTolerations(values ...v1.Toleration) *NodePlacementApplyConfiguration {
	for i := range values {
		b.Tolerations = append(b.Tolerations, values[i])
	}
	return b
}
