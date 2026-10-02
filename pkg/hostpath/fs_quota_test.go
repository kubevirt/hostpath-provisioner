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

	// Fake pool directory. Mapping files live in the temp dir from TestMain, not /etc.
	pool, err := os.MkdirTemp("", "hpp-pool")
	Expect(err).ToNot(HaveOccurred())
	defer os.RemoveAll(pool)

	// Two volumes on a 10Gi pool: pvc-a promises 1024 bytes, pvc-b promises 2048.
	volA := "pvc-a"
	volB := "pvc-b"
	pathA := filepath.Join(pool, volA)
	pathB := filepath.Join(pool, volB)
	const poolCap int64 = 10 * 1024 * 1024 * 1024

	// Record both volumes and check they got different project IDs.
	idA, err := assignProjectId(pathA, volA, 1024, poolCap)
	Expect(err).ToNot(HaveOccurred())
	idB, err := assignProjectId(pathB, volB, 2048, poolCap)
	Expect(err).ToNot(HaveOccurred())
	Expect(idA).ToNot(Equal(idB))

	// Delete only pvc-a. pvc-b must stay.
	err = removeProjectId(pathA, volA)
	Expect(err).ToNot(HaveOccurred())

	// /etc/projid no longer names pvc-a, and still names pvc-b.
	projid, err := os.ReadFile(projidFile)
	Expect(err).ToNot(HaveOccurred())
	Expect(string(projid)).ToNot(ContainSubstring(hppPrefix + volA))
	Expect(string(projid)).To(ContainSubstring(hppPrefix + volB))

	// /etc/projects no longer has pvc-a's path, and still has pvc-b's.
	projects, err := os.ReadFile(projectsFile)
	Expect(err).ToNot(HaveOccurred())
	Expect(string(projects)).ToNot(ContainSubstring(pathA))
	Expect(string(projects)).To(ContainSubstring(pathB))

	// .hpp-quotas dropped pvc-a's promised bytes and kept pvc-b's.
	quotas, err := os.ReadFile(filepath.Join(pool, projQuotasFile))
	Expect(err).ToNot(HaveOccurred())
	Expect(string(quotas)).ToNot(ContainSubstring(volA + ":"))
	Expect(string(quotas)).To(ContainSubstring(volB + ":"))

	// Deleting pvc-a again is a no-op.
	err = removeProjectId(pathA, volA)
	Expect(err).ToNot(HaveOccurred())

	// Only pvc-b's 2048 bytes are still promised.
	committed, err := parseCommitted(filepath.Join(pool, projQuotasFile))
	Expect(err).ToNot(HaveOccurred())
	Expect(committed).To(Equal(int64(2048)))

	// Path lookup no longer finds pvc-a, and pvc-b still has its original ID.
	_, byPath, err := parseProjectMaps()
	Expect(err).ToNot(HaveOccurred())
	_, hasA := byPath[pathA]
	Expect(hasA).To(BeFalse())
	Expect(byPath[pathB]).To(Equal(idB))

	// The rewritten projid file is still a normal text file (ends with a newline).
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
