/*
Copyright 2018 The Kubernetes Authors.

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

package main

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"
)

func getKubevirtNodeAnnotation(value string) map[string]string {
	annotation := make(map[string]string)
	if value != "" {
		annotation["kubevirt.io/provisionOnNode"] = value
	}
	return annotation
}

func Test_isCorrectNode(t *testing.T) {
	type args struct {
		annotations map[string]string
		nodeName    string
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "passes with correct node annotation",
			args: args{
				annotations: getKubevirtNodeAnnotation("test-node"),
				nodeName:    "test-node",
			},
			want: true,
		},
		{
			name: "skips with incorrect node annotation",
			args: args{
				annotations: getKubevirtNodeAnnotation("test-node"),
				nodeName:    "wrong-node",
			},
			want: false,
		},
		{
			name: "skips with no node annotation",
			args: args{
				annotations: getKubevirtNodeAnnotation(""),
				nodeName:    "test-node",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isCorrectNode(tt.args.annotations, tt.args.nodeName); got != tt.want {
				t.Errorf("isCorrectNode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_calculatePvCapacity(t *testing.T) {
	type args struct {
		path string
	}
	tests := []struct {
		name    string
		args    args
		want    *resource.Quantity
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := calculatePvCapacity(tt.args.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("calculatePvCapacity() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("calculatePvCapacity() = %v, want %v", got, tt.want)
			}
		})
	}
}
