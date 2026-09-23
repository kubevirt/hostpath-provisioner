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
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/onsi/gomega"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hpp-quota-maps")
	if err != nil {
		panic(err)
	}
	projidFile = filepath.Join(dir, "projid")
	projectsFile = filepath.Join(dir, "projects")
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func Test_removeProjectId(t *testing.T) {
	RegisterTestingT(t)

	pool, err := os.MkdirTemp("", "hpp-pool")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(pool)

	volA := "pvc-a"
	volB := "pvc-b"
	pathA := filepath.Join(pool, volA)
	pathB := filepath.Join(pool, volB)
	const poolCap int64 = 10 * 1024 * 1024 * 1024

	idA, err := assignProjectId(pathA, volA, 1024, poolCap)
	Expect(err).ToNot(HaveOccurred())
	idB, err := assignProjectId(pathB, volB, 2048, poolCap)
	Expect(err).ToNot(HaveOccurred())
	Expect(idA).ToNot(Equal(idB))

	err = removeProjectId(pathA, volA)
	Expect(err).ToNot(HaveOccurred())

	projid, err := os.ReadFile(projidFile)
	Expect(err).ToNot(HaveOccurred())
	Expect(string(projid)).ToNot(ContainSubstring(hppPrefix + volA))
	Expect(string(projid)).To(ContainSubstring(hppPrefix + volB))

	projects, err := os.ReadFile(projectsFile)
	Expect(err).ToNot(HaveOccurred())
	Expect(string(projects)).ToNot(ContainSubstring(pathA))
	Expect(string(projects)).To(ContainSubstring(pathB))

	quotas, err := os.ReadFile(filepath.Join(pool, projQuotasFile))
	Expect(err).ToNot(HaveOccurred())
	Expect(string(quotas)).ToNot(ContainSubstring(volA + ":"))
	Expect(string(quotas)).To(ContainSubstring(volB + ":"))

	// Idempotent: second delete is a no-op.
	err = removeProjectId(pathA, volA)
	Expect(err).ToNot(HaveOccurred())

	committed, err := parseCommitted(filepath.Join(pool, projQuotasFile))
	Expect(err).ToNot(HaveOccurred())
	Expect(committed).To(Equal(int64(2048)))

	_, byPath, err := parseProjectMaps()
	Expect(err).ToNot(HaveOccurred())
	_, hasA := byPath[pathA]
	Expect(hasA).To(BeFalse())
	Expect(byPath[pathB]).To(Equal(idB))

	// Comments in mapping files are preserved.
	Expect(strings.Count(string(projid), "\n")).To(BeNumerically(">=", 1))
}

func Test_removeProjectId_missingFiles(t *testing.T) {
	RegisterTestingT(t)

	err := os.WriteFile(projidFile, nil, 0644)
	Expect(err).ToNot(HaveOccurred())
	err = os.WriteFile(projectsFile, nil, 0644)
	Expect(err).ToNot(HaveOccurred())

	err = removeProjectId("/no/such/pvc-dir", "pvc-missing")
	Expect(err).ToNot(HaveOccurred())
}
