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

package hostpath

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	. "github.com/onsi/gomega"
)

func Test_parse(t *testing.T) {
	RegisterTestingT(t)
	tests := []struct {
		name       string
		ep         string
		wantScheme string
		wantEp     string
		wantErr    error
	}{
		{
			name:       "valid unix endpoint",
			ep:         "unix://test/test.sock",
			wantScheme: "unix",
			wantEp:     "test/test.sock",
			wantErr:    nil,
		},
		{
			name:       "invalid empty endpoint",
			ep:         "unix://",
			wantScheme: "",
			wantEp:     "",
			wantErr:    fmt.Errorf("invalid endpoint: unix://"),
		},
		{
			name:       "valid tcp endpoint",
			ep:         "tcp://10.10.10.10/test.sock",
			wantScheme: "tcp",
			wantEp:     "10.10.10.10/test.sock",
			wantErr:    nil,
		},
		{
			name:       "missing assume unix",
			ep:         "test/test.sock",
			wantScheme: "unix",
			wantEp:     "test/test.sock",
			wantErr:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, ep, err := parse(tt.ep)
			if tt.wantErr == nil {
				Expect(err).ToNot(HaveOccurred())
			} else {
				Expect(err).To(BeEquivalentTo(tt.wantErr))
			}
			Expect(scheme).To(Equal(tt.wantScheme))
			Expect(ep).To(Equal(tt.wantEp))
		})
	}
}

func Test_listen(t *testing.T) {
	RegisterTestingT(t)
	tempDir, err := ioutil.TempDir(os.TempDir(), "")
	if err != nil {
		t.Errorf("Failed to create tempDir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	listener, closer, err := listen(fmt.Sprintf("unix:/%s/test.sock", tempDir))
	Expect(err).ToNot(HaveOccurred())
	defer closer()
	defer listener.Close()
	Expect(listener.Addr().Network()).To(Equal("unix"))
}
